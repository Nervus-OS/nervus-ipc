package sdk

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// quietConfig 是指向测试替身的配置，日志压到 Error 以免刷屏。
func quietConfig(sock string) Config {
	return Config{
		SockPath:    sock,
		ComponentID: testComponentID,
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestConnect_RequiresComponentID(t *testing.T) {
	// 留空会让 nervud 无法把核对收敛成一次比对，握手必被拒。与其等到网络往返
	// 才失败，不如在构造时就拦住。
	if _, err := Connect(Config{SockPath: "/nonexistent"}); err == nil {
		t.Fatal("empty ComponentID must be rejected before dialing")
	}
}

func TestConnect_HandshakeSuccess(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(nil))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	id := c.Identity()
	if id.PackageID != testPackageID || id.ComponentID != testComponentID {
		t.Errorf("identity = %+v, want %s/%s", id, testPackageID, testComponentID)
	}
	// 预算必须被收下：SDK 靠它自律（比如不发超过 max_frame_bytes 的请求），
	// 丢掉就只能靠「被断开」来发现上限。
	if c.Limits().GetMaxFrameBytes() != MaxFrameBytes {
		t.Errorf("limits not captured: %+v", c.Limits())
	}
}

func TestConnect_FirstFrameIsHelloWithVersionRange(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(nil))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	f.mu.Lock()
	first := f.received[0]
	f.mu.Unlock()

	h := first.GetHello()
	if h == nil {
		t.Fatalf("first frame is %T, want Hello", first.GetBody())
	}
	if h.GetMinProtocolMajor() != 1 || h.GetMaxProtocolMajor() != 1 {
		t.Errorf("major range = [%d,%d], want [1,1]", h.GetMinProtocolMajor(), h.GetMaxProtocolMajor())
	}
	if h.GetDeclaredComponentId() != testComponentID {
		t.Errorf("declared component = %q", h.GetDeclaredComponentId())
	}
	// 握手帧【必须】带版本号——全协议只有这里的 protocol_major/minor 有意义。
	if first.GetProtocolMajor() != 1 {
		t.Errorf("Hello envelope protocol_major = %d, want 1", first.GetProtocolMajor())
	}
}

func TestConnect_HandshakeRejected(t *testing.T) {
	// nervud 在身份核对失败时先发 Failure HelloAck 再关连接，不裸关——
	// 裸关会让客户端无法区分「版本/身份不对」和「socket 坏了」，而这两者的
	// 正确反应相反。这里断言 SDK 确实把原因带出来了。
	f := startFakeNervud(t)
	f.setHandler(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if env.GetHello() != nil {
			return []*ipcv1.Envelope{helloAckFailure(ipcv1.StatusCode_STATUS_CODE_UNAUTHENTICATED)}
		}
		return nil
	})

	_, err := Connect(quietConfig(f.sockPath()))
	if !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("err = %v, want ErrHandshakeFailed", err)
	}
	if !contains(err.Error(), "UNAUTHENTICATED") {
		t.Errorf("error should carry the status code, got %v", err)
	}
}

func TestResolveEndpoint_Success(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		r := env.GetResolveEndpoint()
		if r == nil {
			return nil
		}
		return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_ResolveEndpointResult{
			ResolveEndpointResult: &ipcv1.ResolveEndpointResult{
				RequestId: r.GetRequestId(),
				Outcome: &ipcv1.ResolveEndpointResult_Success{
					Success: &ipcv1.ResolveEndpointSuccess{
						EndpointId:     42,
						InterfaceMajor: 1,
						ResourceHandle: "base.main",
					},
				},
			},
		}}}
	}))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	ep, err := c.ResolveEndpoint(context.Background(), ResolveRequest{
		InterfaceID: "nervus.interface.motion.base", MinMajor: 1, MaxMajor: 1,
	})
	if err != nil {
		t.Fatalf("ResolveEndpoint: %v", err)
	}
	if ep.EndpointID != 42 || ep.ResourceHandle != "base.main" {
		t.Fatalf("endpoint = %+v", ep)
	}
}

func TestResolveEndpoint_EmptySelectorIsOmitted(t *testing.T) {
	// 空 selector 与不发 selector 是同一语义（nervud 隐式取 motion.base/main），
	// 但不发少几个字节。断言 SDK 没有画蛇添足地塞一个空消息。
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		r := env.GetResolveEndpoint()
		if r == nil {
			return nil
		}
		if r.GetSelector() != nil {
			t.Errorf("selector should be omitted when both fields empty, got %+v", r.GetSelector())
		}
		return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_ResolveEndpointResult{
			ResolveEndpointResult: &ipcv1.ResolveEndpointResult{
				RequestId: r.GetRequestId(),
				Outcome: &ipcv1.ResolveEndpointResult_Success{
					Success: &ipcv1.ResolveEndpointSuccess{EndpointId: 1},
				},
			},
		}}}
	}))

	c, _ := Connect(quietConfig(f.sockPath()))
	defer func() { _ = c.Close() }()
	if _, err := c.ResolveEndpoint(context.Background(), ResolveRequest{
		InterfaceID: "x", MinMajor: 1, MaxMajor: 1,
	}); err != nil {
		t.Fatalf("ResolveEndpoint: %v", err)
	}
}

func TestResolveEndpoint_TypedErrorDetail(t *testing.T) {
	// 失败必须带 typed detail：调用方要区分「接口不存在」（别重试）和
	// 「资源暂时没有」（可退避重试），笼统一个 FAILED_PRECONDITION 不够。
	detail := &ipcv1.ResolveEndpointErrorDetail{
		Reason: ipcv1.ResolveEndpointReason_RESOLVE_ENDPOINT_REASON_INTERFACE_NOT_FOUND,
	}
	detailBytes := mustMarshal(t, detail)

	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		r := env.GetResolveEndpoint()
		if r == nil {
			return nil
		}
		return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_ResolveEndpointResult{
			ResolveEndpointResult: &ipcv1.ResolveEndpointResult{
				RequestId: r.GetRequestId(),
				Outcome: &ipcv1.ResolveEndpointResult_Failure{Failure: &ipcv1.Failure{
					Code:        ipcv1.StatusCode_STATUS_CODE_FAILED_PRECONDITION,
					ErrorDetail: detailBytes,
				}},
			},
		}}}
	}))

	c, _ := Connect(quietConfig(f.sockPath()))
	defer func() { _ = c.Close() }()

	_, err := c.ResolveEndpoint(context.Background(), ResolveRequest{InterfaceID: "nope", MinMajor: 1, MaxMajor: 1})
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if se.Code != ipcv1.StatusCode_STATUS_CODE_FAILED_PRECONDITION {
		t.Errorf("code = %v", se.Code)
	}
	got := &ipcv1.ResolveEndpointErrorDetail{}
	ok, derr := se.UnmarshalDetail(got)
	if derr != nil || !ok {
		t.Fatalf("UnmarshalDetail: ok=%v err=%v", ok, derr)
	}
	if got.GetReason() != ipcv1.ResolveEndpointReason_RESOLVE_ENDPOINT_REASON_INTERFACE_NOT_FOUND {
		t.Errorf("reason = %v", got.GetReason())
	}
}

func TestCall_SuccessAndFailure(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		r := env.GetRequest()
		if r == nil {
			return nil
		}
		if r.GetMethodId() == 99 {
			return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
				RequestId: r.GetRequestId(),
				Outcome: &ipcv1.Response_Failure{Failure: &ipcv1.Failure{
					Code: ipcv1.StatusCode_STATUS_CODE_PERMISSION_DENIED,
				}},
			}}}}
		}
		return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
			RequestId: r.GetRequestId(),
			Outcome: &ipcv1.Response_Success{Success: &ipcv1.Success{
				Code:    ipcv1.StatusCode_STATUS_CODE_OK,
				Payload: append([]byte("echo:"), r.GetPayload()...),
			}},
		}}}}
	}))

	c, _ := Connect(quietConfig(f.sockPath()))
	defer func() { _ = c.Close() }()

	out, err := c.Call(context.Background(), 1, 7, []byte("hi"), time.Second)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(out) != "echo:hi" {
		t.Fatalf("payload = %q", out)
	}

	_, err = c.Call(context.Background(), 1, 99, nil, time.Second)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != ipcv1.StatusCode_STATUS_CODE_PERMISSION_DENIED {
		t.Fatalf("err = %v, want PERMISSION_DENIED StatusError", err)
	}
	// 权限被拒应当走「重新 Resolve」而不是「原地重试」
	if IsRetryable(err) {
		t.Error("PERMISSION_DENIED must not be retryable")
	}
	if !NeedsReResolve(err) {
		t.Error("PERMISSION_DENIED should trigger re-resolve")
	}
}

func TestCall_OutOfOrderResponses(t *testing.T) {
	// nervud 不保证按请求顺序回。三个并发调用，服务端故意反序应答，
	// 每个调用方都必须拿到自己那一个。
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		r := env.GetRequest()
		if r == nil {
			return nil
		}
		// 用 method_id 当作可辨识的回显内容
		return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
			RequestId: r.GetRequestId(),
			Outcome: &ipcv1.Response_Success{Success: &ipcv1.Success{
				Payload: []byte{byte(r.GetMethodId())},
			}},
		}}}}
	}))

	c, _ := Connect(quietConfig(f.sockPath()))
	defer func() { _ = c.Close() }()

	type res struct {
		method byte
		out    []byte
		err    error
	}
	ch := make(chan res, 3)
	for _, m := range []uint32{11, 22, 33} {
		go func(m uint32) {
			out, err := c.Call(context.Background(), 1, m, nil, time.Second)
			ch <- res{byte(m), out, err}
		}(m)
	}
	for i := 0; i < 3; i++ {
		r := <-ch
		if r.err != nil {
			t.Fatalf("call %d: %v", r.method, r.err)
		}
		if len(r.out) != 1 || r.out[0] != r.method {
			t.Fatalf("call %d got payload % x (crossed wires)", r.method, r.out)
		}
	}
}

func TestCall_LateResponseDoesNotKillConnection(t *testing.T) {
	// 调用方超时走人后，服务端才把响应发回来。SDK 必须丢弃它并保持连接可用——
	// 已超时的请求收到迟到响应是正常时序，当成协议违规会频繁踢掉连接。
	f := startFakeNervud(t)
	var held *ipcv1.Request
	f.setHandler(autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if r := env.GetRequest(); r != nil {
			if held == nil {
				held = r // 第一个请求：扣住不回
				return nil
			}
			// 第二个请求到达时，先把扣住的那个迟到响应放出来，再正常回本次
			return []*ipcv1.Envelope{
				{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
					RequestId: held.GetRequestId(),
					Outcome:   &ipcv1.Response_Success{Success: &ipcv1.Success{}},
				}}},
				{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
					RequestId: r.GetRequestId(),
					Outcome: &ipcv1.Response_Success{Success: &ipcv1.Success{
						Payload: []byte("second"),
					}},
				}}},
			}
		}
		return nil
	}))

	c, _ := Connect(quietConfig(f.sockPath()))
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, 1, 1, nil, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first call err = %v, want DeadlineExceeded", err)
	}

	out, err := c.Call(context.Background(), 1, 2, nil, time.Second)
	if err != nil {
		t.Fatalf("connection unusable after late response: %v", err)
	}
	if string(out) != "second" {
		t.Fatalf("payload = %q", out)
	}
}

func TestCall_ServerDisconnectWakesWaiters(t *testing.T) {
	// 连接断开必须唤醒全部在途请求，否则调用方永久卡住（goroutine 泄漏）。
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if env.GetRequest() != nil {
			_ = c.Close() // 收到请求就断开，不回响应
		}
		return nil
	}))

	c, _ := Connect(quietConfig(f.sockPath()))
	defer func() { _ = c.Close() }()

	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), 1, 1, nil, time.Second)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v, want ErrClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Call did not return after server disconnect")
	}
}

func TestClient_RespondsToServerPing(t *testing.T) {
	// 协议允许任一侧发起 Ping。客户端必须原样回显 nonce，否则服务端会把它
	// 判成失联并断开——保活是双向的。
	f := startFakeNervud(t)
	pong := make(chan uint64, 1)
	f.setHandler(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if env.GetHello() != nil {
			return []*ipcv1.Envelope{
				helloAckOK(),
				{Body: &ipcv1.Envelope_Ping{Ping: &ipcv1.Ping{Nonce: 0xDEADBEEF}}},
			}
		}
		if p := env.GetPong(); p != nil {
			select {
			case pong <- p.GetNonce():
			default:
			}
		}
		return nil
	})

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	select {
	case n := <-pong:
		if n != 0xDEADBEEF {
			t.Fatalf("pong nonce = %#x, want 0xDEADBEEF", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not answer server Ping")
	}
}

func TestClient_UnexpectedBodyClosesConnection(t *testing.T) {
	// Dispatch 只该出现在 Service 连接上。客户端收到它说明 nervud 状态机错乱
	// 或对端不是 nervud——必须关连接，不能静默忽略。
	f := startFakeNervud(t)
	f.setHandler(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if env.GetHello() != nil {
			return []*ipcv1.Envelope{
				helloAckOK(),
				{Body: &ipcv1.Envelope_Dispatch{Dispatch: &ipcv1.Dispatch{RouteId: 1}}},
			}
		}
		return nil
	})

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	deadline := time.After(3 * time.Second)
	for {
		_, err := c.Call(context.Background(), 1, 1, nil, 200*time.Millisecond)
		if errors.Is(err, ErrClosed) {
			return // 期望：连接已被关闭
		}
		select {
		case <-deadline:
			t.Fatalf("connection stayed open after unexpected body, last err = %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
