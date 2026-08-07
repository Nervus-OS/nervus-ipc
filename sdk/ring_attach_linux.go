//go:build linux

package sdk

import (
	"fmt"
	"net"
	"syscall"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
	"golang.org/x/sys/unix"
)

// SHARED_MEMORY_RING 在 SDK 侧的附着：收 fd、映射内存、建 Ring 视图。
//
// Linux only：memfd 与 SCM_RIGHTS 都没有跨平台等价物，而 nervud 本来就只跑
// Linux。这里【不提供非 Linux 兜底】——一个「能编译但每次调用都失败」的假实现
// 只会把「这需要 Linux」从编译期事实退化成运行期意外。

// attachRing 收下 nervud 送来的两个 fd 并映射 ring。
//
// 顺序固定为 memfd, eventfd（见 transfer.proto）。收到的数量不等于 2 视为协议
// 违规——数量不符说明两侧对 ABI 的理解已经分叉，继续下去只会读到错误的内存。
func (c *TransferConn) attachRing(raw net.Conn, cfg *ipcv1.TransferRingConfig) error {
	geometry, err := ValidateRingConfig(cfg)
	if err != nil {
		return err
	}
	total, err := geometry.TotalBytes()
	if err != nil {
		return err
	}

	unixConn, ok := raw.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("%w: shared memory ring requires a unix socket", ErrProtocol)
	}

	// 两个 fd 的控制消息空间。多要一点无妨；少了会被内核截断，而截断的表现是
	// 「fd 莫名少一个」，比一个明确的错误难查得多
	oob := make([]byte, unix.CmsgSpace(2*4))
	payload := make([]byte, 1)
	_, oobn, _, _, err := unixConn.ReadMsgUnix(payload, oob)
	if err != nil {
		return fmt.Errorf("sdk: receive ring descriptors: %w", err)
	}

	fds, err := parseUnixRights(oob[:oobn])
	if err != nil {
		closeFDs(fds)
		return err
	}
	if len(fds) != 2 {
		closeFDs(fds)
		return fmt.Errorf("%w: expected exactly 2 ring descriptors, got %d",
			ErrProtocol, len(fds))
	}
	memFD, eventFD := fds[0], fds[1]

	// 映射长度取自【控制面下发的几何】，不是 fstat 出来的大小：后者由对端
	// 可影响的 memfd 决定，用它算边界等于把边界交给不可信输入
	prot := unix.PROT_READ
	if c.role == ipcv1.TransferRole_TRANSFER_ROLE_PRODUCER ||
		c.role == ipcv1.TransferRole_TRANSFER_ROLE_PEER {
		prot |= unix.PROT_WRITE
	} else {
		// 消费者也要写 consumer_cursor，因此仍需 PROT_WRITE。
		// 保留这个分支是为了让「谁能写什么」显式可见：真正的隔离在
		// Ring 的角色判定与 seqlock 上，不在页保护上
		prot |= unix.PROT_WRITE
	}
	mem, err := unix.Mmap(memFD, 0, int(total), prot, unix.MAP_SHARED)
	if err != nil {
		closeFDs(fds)
		return fmt.Errorf("sdk: mmap ring: %w", err)
	}

	producer := c.role == ipcv1.TransferRole_TRANSFER_ROLE_PRODUCER ||
		c.role == ipcv1.TransferRole_TRANSFER_ROLE_PEER
	ring, err := NewRing(mem, geometry, producer)
	if err != nil {
		_ = unix.Munmap(mem)
		closeFDs(fds)
		return err
	}

	c.ring = ring
	c.ringMem = mem
	c.ringMemFD = memFD
	c.ringEventFD = eventFD
	return nil
}

// parseUnixRights 从控制消息里取出 fd。
func parseUnixRights(oob []byte) ([]int, error) {
	if len(oob) == 0 {
		return nil, fmt.Errorf("%w: no control message with ring descriptors", ErrProtocol)
	}
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, fmt.Errorf("%w: parse ring control message: %v", ErrProtocol, err)
	}
	var out []int
	for _, msg := range msgs {
		if msg.Header.Level != unix.SOL_SOCKET || msg.Header.Type != unix.SCM_RIGHTS {
			continue
		}
		fds, err := unix.ParseUnixRights(&msg)
		if err != nil {
			closeFDs(out)
			return nil, fmt.Errorf("%w: parse ring rights: %v", ErrProtocol, err)
		}
		out = append(out, fds...)
	}
	return out, nil
}

func closeFDs(fds []int) {
	for _, fd := range fds {
		_ = syscall.Close(fd)
	}
}

// closeRing 释放映射与两个 fd。多次调用安全。
func (c *TransferConn) closeRing() {
	if c.ringMem != nil {
		_ = unix.Munmap(c.ringMem)
		c.ringMem = nil
	}
	if c.ringMemFD > 0 {
		_ = syscall.Close(c.ringMemFD)
		c.ringMemFD = -1
	}
	if c.ringEventFD > 0 {
		_ = syscall.Close(c.ringEventFD)
		c.ringEventFD = -1
	}
	c.ring = nil
}

// notifyRing 唤醒对端。生产者写完一帧后调用。
func (c *TransferConn) notifyRing() error {
	if c.ringEventFD <= 0 {
		return nil
	}
	var one [8]byte
	one[7] = 1
	if _, err := unix.Write(c.ringEventFD, one[:]); err != nil &&
		err != unix.EAGAIN {
		return fmt.Errorf("sdk: notify ring peer: %w", err)
	}
	return nil
}

// waitRing 阻塞等待对端通知，或直到 timeoutMillis 到期（负数表示无限等）。
func (c *TransferConn) waitRing(timeoutMillis int) error {
	if c.ringEventFD <= 0 {
		return fmt.Errorf("%w: transfer has no ring event descriptor", ErrProtocol)
	}
	fds := []unix.PollFd{{Fd: int32(c.ringEventFD), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, timeoutMillis)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return fmt.Errorf("sdk: wait ring: %w", err)
		}
		if n == 0 {
			return nil // 超时：调用方自己决定是重试还是放弃
		}
		var drain [8]byte
		_, _ = unix.Read(c.ringEventFD, drain[:])
		return nil
	}
}
