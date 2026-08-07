//go:build linux

package sdk

import (
	"bytes"
	"errors"
	"os"
	"testing"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
	"golang.org/x/sys/unix"
)

// 端到端：真的建 memfd、真的 mmap、真的跨映射收发。
//
// 前面的 ring_test.go 用一段普通 []byte 模拟共享内存，验的是 ABI 逻辑；
// 本文件验的是【系统调用这一层真的成立】——memfd 能建、能 ftruncate、能封口、
// 两次独立 mmap 看到的是同一块物理内存。
func TestRingOverRealMemfd(t *testing.T) {
	g := RingGeometry{
		SlotCount: 4, SlotSize: 128,
		HeaderBytes: RingHeaderBytes, DescriptorBytes: RingDescriptorBytes,
	}
	total, err := g.TotalBytes()
	if err != nil {
		t.Fatalf("TotalBytes: %v", err)
	}

	fd, err := unix.MemfdCreate("ring-e2e", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Skipf("memfd_create 不可用: %v", err)
	}
	memFile := os.NewFile(uintptr(fd), "ring-e2e")
	defer func() { _ = memFile.Close() }()

	if err := unix.Ftruncate(int(memFile.Fd()), int64(total)); err != nil {
		t.Fatalf("Ftruncate: %v", err)
	}

	// 两次独立映射，模拟两个进程各自 mmap 同一个 memfd
	producerMem, err := unix.Mmap(int(memFile.Fd()), 0, int(total),
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		t.Fatalf("mmap producer: %v", err)
	}
	defer func() { _ = unix.Munmap(producerMem) }()

	consumerMem, err := unix.Mmap(int(memFile.Fd()), 0, int(total),
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		t.Fatalf("mmap consumer: %v", err)
	}
	defer func() { _ = unix.Munmap(consumerMem) }()

	if err := InitHeader(producerMem, g); err != nil {
		t.Fatalf("InitHeader: %v", err)
	}
	producer, err := NewRing(producerMem, g, true)
	if err != nil {
		t.Fatalf("NewRing producer: %v", err)
	}
	consumer, err := NewRing(consumerMem, g, false)
	if err != nil {
		t.Fatalf("NewRing consumer: %v", err)
	}

	// 跨两个独立映射收发：写进 producerMem 的内容必须出现在 consumerMem
	for i := 0; i < 16; i++ {
		payload := bytes.Repeat([]byte{byte(i)}, 64)
		if err := producer.Write(payload, uint32(i), uint64(i)*1000); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		frame, err := consumer.Read()
		if err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		if !bytes.Equal(frame.Payload, payload) {
			t.Fatalf("第 %d 帧内容不符", i)
		}
		if frame.Sequence != uint64(i) || frame.Flags != uint32(i) {
			t.Fatalf("第 %d 帧元数据 = %+v", i, frame)
		}
		consumer.Advance()
	}
}

// 封口之后不能再改大小——两端因此可以信任「映射长度 == 控制面几何」。
func TestRingMemfdSealPreventsResize(t *testing.T) {
	fd, err := unix.MemfdCreate("ring-seal", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Skipf("memfd_create 不可用: %v", err)
	}
	f := os.NewFile(uintptr(fd), "ring-seal")
	defer func() { _ = f.Close() }()

	if err := unix.Ftruncate(int(f.Fd()), 8192); err != nil {
		t.Fatalf("Ftruncate: %v", err)
	}
	if _, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS,
		unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_SEAL); err != nil {
		t.Fatalf("F_ADD_SEALS: %v", err)
	}
	if err := unix.Ftruncate(int(f.Fd()), 4096); err == nil {
		t.Fatal("封口之后仍能缩小 memfd")
	}
	if err := unix.Ftruncate(int(f.Fd()), 16384); err == nil {
		t.Fatal("封口之后仍能扩大 memfd")
	}
}

// SCM_RIGHTS 传两个 fd 并按固定顺序取回。数量不符必须报错——
// 数量不符说明两侧对 ABI 的理解已经分叉。
func TestRingDescriptorHandover(t *testing.T) {
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("Socketpair: %v", err)
	}
	sendFile := os.NewFile(uintptr(pair[0]), "send")
	recvFile := os.NewFile(uintptr(pair[1]), "recv")
	defer func() { _ = sendFile.Close(); _ = recvFile.Close() }()

	memFD, err := unix.MemfdCreate("ring-fd", unix.MFD_CLOEXEC)
	if err != nil {
		t.Skipf("memfd_create 不可用: %v", err)
	}
	defer func() { _ = unix.Close(memFD) }()
	eventFD, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		t.Fatalf("Eventfd: %v", err)
	}
	defer func() { _ = unix.Close(eventFD) }()

	rights := unix.UnixRights(memFD, eventFD)
	if err := unix.Sendmsg(pair[0], []byte{0}, rights, nil, 0); err != nil {
		t.Fatalf("Sendmsg: %v", err)
	}

	oob := make([]byte, unix.CmsgSpace(2*4))
	payload := make([]byte, 1)
	_, oobn, _, _, err := unix.Recvmsg(pair[1], payload, oob, 0)
	if err != nil {
		t.Fatalf("Recvmsg: %v", err)
	}

	fds, err := parseUnixRights(oob[:oobn])
	if err != nil {
		t.Fatalf("parseUnixRights: %v", err)
	}
	defer closeFDs(fds)
	if len(fds) != 2 {
		t.Fatalf("收到 %d 个 fd, want 2", len(fds))
	}
	// 收到的 fd 必须真的可用：对 memfd 做一次 fstat
	var st unix.Stat_t
	if err := unix.Fstat(fds[0], &st); err != nil {
		t.Fatalf("收到的 memfd 不可用: %v", err)
	}
}

// 没有控制消息时必须报协议错误，不能当成「零个 fd」继续。
func TestParseUnixRightsRejectsEmpty(t *testing.T) {
	if _, err := parseUnixRights(nil); !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want ErrProtocol", err)
	}
}

// 几何参数不合法时不该走到映射那一步。
func TestAttachRingRejectsBadGeometry(t *testing.T) {
	c := &TransferConn{role: ipcv1.TransferRole_TRANSFER_ROLE_CONSUMER}
	err := c.attachRing(nil, &ipcv1.TransferRingConfig{
		SlotCount: 3, SlotSize: 64, // 3 不是 2 的幂
		HeaderBytes: RingHeaderBytes, DescriptorBytes: RingDescriptorBytes,
	})
	if !errors.Is(err, ErrRingGeometry) {
		t.Fatalf("err = %v, want ErrRingGeometry", err)
	}
}
