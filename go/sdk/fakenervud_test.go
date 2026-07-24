package sdk

import (
	"net"
	"path/filepath"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	ipcv1 "github.com/nervus-os/nervus-ipc/go/protocol/ipcv1"
)

// fakeNervud 是 nervud 控制面的测试替身：一条 UDS 上按 nervud 的状态机应答。
//
// 它【不是】nervud 的第二实现，只覆盖 SDK 需要断言的那部分往返：握手、
// ResolveEndpoint、Request→Response、Dispatch→DispatchResult。行为上刻意与
// internal/ipc 的关键纪律保持一致（首帧必须 Hello、身份由服务端回显、
// 未实现 body 关连接），这样 SDK 在这里过了，接真 nervud 才有意义。
//
// 对标 Python SDK 曾有的 _fakenervud.py。
type fakeNervud struct {
	t  *testing.T
	ln net.Listener

	mu sync.Mutex
	// onEnvelope 收到一个 Envelope 时调用，返回要回发的 Envelope（nil = 不回）。
	// 测试用它注入各种应答与异常。
	onEnvelope func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope

	// received 记录服务端收到的全部 Envelope，供断言。
	received []*ipcv1.Envelope

	wg     sync.WaitGroup
	closed bool
}

// startFakeNervud 在临时目录里起一条 UDS。
//
// 用真 socket 而不是 net.Pipe：dial 里写死了 network="unix"，而那个写死本身是
// 有意的（控制面身份靠 SO_PEERCRED，TCP 上没有等价物）。测试跟着走真路径。
func startFakeNervud(t *testing.T) *fakeNervud {
	t.Helper()
	// UDS 路径有长度上限（sun_path 约 108 字节），t.TempDir() 在某些环境下
	// 会很长，所以只取一个短文件名。
	sock := filepath.Join(t.TempDir(), "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeNervud{t: t, ln: ln}
	f.wg.Add(1)
	go f.acceptLoop()
	t.Cleanup(f.stop)
	return f
}

func (f *fakeNervud) sockPath() string { return f.ln.Addr().String() }

func (f *fakeNervud) setHandler(fn func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope) {
	f.mu.Lock()
	f.onEnvelope = fn
	f.mu.Unlock()
}

func (f *fakeNervud) stop() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	f.mu.Unlock()
	_ = f.ln.Close()
	f.wg.Wait()
}

func (f *fakeNervud) acceptLoop() {
	defer f.wg.Done()
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return // listener 关闭
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			f.serveConn(c)
		}()
	}
}

func (f *fakeNervud) serveConn(c net.Conn) {
	defer func() { _ = c.Close() }()
	buf := make([]byte, MaxFrameBytes)
	for {
		n, err := readFrameHeader(c)
		if err != nil {
			return
		}
		body, err := readFrameBody(c, buf, n)
		if err != nil {
			return
		}
		env := &ipcv1.Envelope{}
		if err := proto.Unmarshal(body, env); err != nil {
			return
		}

		f.mu.Lock()
		f.received = append(f.received, env)
		fn := f.onEnvelope
		f.mu.Unlock()

		if fn == nil {
			continue
		}
		for _, out := range fn(c, env) {
			if out == nil {
				continue
			}
			b, err := proto.Marshal(out)
			if err != nil {
				return
			}
			if err := writeFrame(c, b); err != nil {
				return
			}
		}
	}
}

// recvCount 返回服务端至今收到的 Envelope 数。
func (f *fakeNervud) recvCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.received)
}

// ---- 常用应答构造 ---------------------------------------------------------

const (
	testPackageID   = "com.example.test"
	testComponentID = "main"
)

// helloAckOK 是标准的成功握手应答。
func helloAckOK() *ipcv1.Envelope {
	return &ipcv1.Envelope{Body: &ipcv1.Envelope_HelloAck{HelloAck: &ipcv1.HelloAck{
		Outcome: &ipcv1.HelloAck_Success{Success: &ipcv1.HelloAckSuccess{
			ProtocolMajor: 1,
			ProtocolMinor: 0,
			PackageId:     testPackageID,
			ComponentId:   testComponentID,
			Limits: &ipcv1.ConnectionLimits{
				MaxFrameBytes:             MaxFrameBytes,
				MaxInflightRequests:       64,
				DefaultTimeoutMs:          1000,
				MaxTimeoutMs:              30000,
				MaxSubscriptions:          8,
				IdleTimeoutMs:             60000,
				MaxInflightPayloadBytes:   1 << 20,
				MaxOutboundQueueBytes:     512 << 10,
				DefaultMethodPayloadBytes: 16 << 10,
			},
		}},
	}}}
}

// helloAckFailure 是被拒的握手应答。
func helloAckFailure(code ipcv1.StatusCode) *ipcv1.Envelope {
	return &ipcv1.Envelope{Body: &ipcv1.Envelope_HelloAck{HelloAck: &ipcv1.HelloAck{
		Outcome: &ipcv1.HelloAck_Failure{Failure: &ipcv1.Failure{Code: code}},
	}}}
}

// autoHandshake 返回一个只负责握手、其余交给 next 的 handler。
func autoHandshake(next func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope) func(net.Conn, *ipcv1.Envelope) []*ipcv1.Envelope {
	return func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if env.GetHello() != nil {
			return []*ipcv1.Envelope{helloAckOK()}
		}
		if next == nil {
			return nil
		}
		return next(c, env)
	}
}
