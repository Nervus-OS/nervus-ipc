// 本文件是 Client 与 ServiceHost 共用的连接层：拨号、Envelope 读写、关闭。
//
// 它之上是两种角色（消费侧 Client / 提供侧 ServiceHost），之下是 wire.go 的分帧。
// 抽出来是因为两种角色的连接语义完全相同——握手、读循环、写串行化、关闭一次性——
// 差别只在读到 Envelope 之后怎么分派。
package sdk

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// DefaultSockPath 是 nervud 控制面 UDS 的固定路径（生产镜像）。
//
// 与管理通道 /run/nervus/nervud-admin.sock 是两条独立的 socket：那条只接受
// root（运维工具 nervusctl），本条只接受 App 段 UID。别接错。
const DefaultSockPath = "/run/nervus/nervud.sock"

// 本 SDK 实现的协议版本。
//
// 与 nervud 的 internal/ipc 常量对齐：只实现 major 1；minor 只增不减，握手时
// 由服务端在客户端声明的范围内取交集。
const (
	protocolMajorMin              = 1
	protocolMajorMax              = 1
	protocolMinorMax              = 1
	executionContextProtocolMinor = 1
)

// conn 是一条已建立（但未必已握手）的控制面连接。
type conn struct {
	c net.Conn

	// 写侧：bufio 让长度前缀与正文合并成一次系统调用（UDS 没有 Nagle，拆成
	// 两次不会有延迟病理，只是白白多一次 syscall）。writeMu 串行化并发写——
	// writeFrame 自己不加锁，两个 goroutine 并发写会让 Frame 字节交错。
	w       *bufio.Writer
	writeMu sync.Mutex

	// readBuf 按连接复用，稳态下读路径零堆分配。容量固定为硬上限，这样
	// readFrameBody 永远不会因为 buf 太小而失败。
	readBuf []byte

	// 握手协商结果，握手完成后只读。
	negMajor, negMinor uint32
	packageID          string
	componentID        string
	limits             *ipcv1.ConnectionLimits

	closeOnce sync.Once
	closed    chan struct{}
}

// dial 连接 nervud 的控制面 UDS。
//
// 只支持 unix domain socket：控制面身份靠 SO_PEERCRED 从内核取得，TCP 上没有
// 等价物。把它做成可配置的 network 参数只会诱使人在 TCP 上跑，而那会让整个
// 身份模型失效。
func dial(sockPath string, timeout time.Duration) (*conn, error) {
	if sockPath == "" {
		sockPath = DefaultSockPath
	}
	c, err := net.DialTimeout("unix", sockPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("sdk: dial %s: %w", sockPath, err)
	}
	return &conn{
		c:       c,
		w:       bufio.NewWriterSize(c, 4096),
		readBuf: make([]byte, MaxFrameBytes),
		closed:  make(chan struct{}),
	}, nil
}

// writeEnvelope 序列化并写出一个 Envelope，全程持写锁。
//
// 握手完成后【不】回填 protocol_major/minor：协议版本是连接属性，握手后由连接
// 状态决定，接收方忽略后续 Envelope 的这两个字段（envelope.proto 明文规定）。
// proto3 不发零值，所以留空的这两个字段一个字节都不占。
func (co *conn) writeEnvelope(env *ipcv1.Envelope) error {
	b, err := proto.Marshal(env)
	if err != nil {
		return fmt.Errorf("sdk: marshal envelope: %w", err)
	}

	co.writeMu.Lock()
	defer co.writeMu.Unlock()

	select {
	case <-co.closed:
		return ErrClosed
	default:
	}

	if err := writeFrame(co.w, b); err != nil {
		// 写失败后这条流已不可恢复（长度可能已出去而正文没有），直接废掉连接
		co.close()
		return err
	}
	if err := co.w.Flush(); err != nil {
		co.close()
		return fmt.Errorf("sdk: flush: %w", err)
	}
	return nil
}

// readEnvelope 读一个完整 Envelope。
//
// 【调用约束】只能有一个 reader goroutine 调用本函数：readBuf 是复用的，返回的
// Envelope 里的 bytes 字段可能引用它（protobuf 解码会拷贝，但别依赖这一点），
// 并发读还会让两个 goroutine 抢同一个字节流的边界。
func (co *conn) readEnvelope() (*ipcv1.Envelope, error) {
	n, err := readFrameHeader(co.c)
	if err != nil {
		return nil, err
	}
	body, err := readFrameBody(co.c, co.readBuf, n)
	if err != nil {
		return nil, err
	}
	env := &ipcv1.Envelope{}
	if err := proto.Unmarshal(body, env); err != nil {
		return nil, fmt.Errorf("%w: malformed envelope: %v", ErrProtocol, err)
	}
	if env.GetBody() == nil {
		// body 未设置（含来自未知新版本的未知 body）一律视为协议违规。
		// 空 Envelope 没有合法用途，容忍它等于给「发一堆空帧刷预算」留口子。
		return nil, fmt.Errorf("%w: envelope has no body", ErrProtocol)
	}
	return env, nil
}

// setReadDeadline 设置下一次读的截止时间。零值清除。
func (co *conn) setReadDeadline(t time.Time) error { return co.c.SetReadDeadline(t) }

// close 幂等关闭底层连接。
func (co *conn) close() {
	co.closeOnce.Do(func() {
		close(co.closed)
		_ = co.c.Close()
	})
}

// Limits 返回握手时 nervud 下发的本连接预算。
//
// 这些值是【SDK 自律的依据，不是服务端的承诺】：服务端不会因为告知过就放松
// 执法。超了照样被拒绝或被断开。
func (co *conn) Limits() *ipcv1.ConnectionLimits { return co.limits }
