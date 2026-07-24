package sdk

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	ipcv1 "github.com/nervus-os/nervus-ipc/go/protocol/ipcv1"
)

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestServiceHost_RegisterEndpoint(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		r := env.GetRegisterEndpoint()
		if r == nil {
			return nil
		}
		if r.GetInterfaceId() != "nervus.interface.motion.base" {
			t.Errorf("interface_id = %q", r.GetInterfaceId())
		}
		if r.GetResourceHandle() != "base.main" {
			t.Errorf("resource_handle = %q", r.GetResourceHandle())
		}
		return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_RegisterEndpointResult{
			RegisterEndpointResult: &ipcv1.RegisterEndpointResult{
				RequestId: r.GetRequestId(),
				Outcome: &ipcv1.RegisterEndpointResult_Success{
					Success: &ipcv1.RegisterEndpointSuccess{EndpointId: 3},
				},
			},
		}}}
	}))

	h, err := NewServiceHost(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("NewServiceHost: %v", err)
	}
	defer func() { _ = h.Close() }()

	epID, err := h.RegisterEndpoint(context.Background(), RegisterRequest{
		InterfaceID: "nervus.interface.motion.base", Major: 1, ResourceHandle: "base.main",
	})
	if err != nil {
		t.Fatalf("RegisterEndpoint: %v", err)
	}
	if epID != 3 {
		t.Fatalf("endpoint id = %d, want 3", epID)
	}
}

// registerThen 应答握手与报到，之后把其余 Envelope 交给 next。
func registerThen(next func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope) func(net.Conn, *ipcv1.Envelope) []*ipcv1.Envelope {
	return autoHandshake(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if r := env.GetRegisterEndpoint(); r != nil {
			return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_RegisterEndpointResult{
				RegisterEndpointResult: &ipcv1.RegisterEndpointResult{
					RequestId: r.GetRequestId(),
					Outcome: &ipcv1.RegisterEndpointResult_Success{
						Success: &ipcv1.RegisterEndpointSuccess{EndpointId: 1},
					},
				},
			}}}
		}
		if next == nil {
			return nil
		}
		return next(c, env)
	})
}

func TestServiceHost_DispatchSuccess(t *testing.T) {
	f := startFakeNervud(t)
	results := make(chan *ipcv1.DispatchResult, 1)
	f.setHandler(registerThen(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if dr := env.GetDispatchResult(); dr != nil {
			results <- dr
			return nil
		}
		return nil
	}))

	h, err := NewServiceHost(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("NewServiceHost: %v", err)
	}
	defer func() { _ = h.Close() }()

	var gotCaller *ipcv1.CallerContext
	h.Handle(5, func(cc CallContext, payload []byte) ([]byte, error) {
		gotCaller = cc.Caller
		return append([]byte("ok:"), payload...), nil
	})

	if _, err := h.RegisterEndpoint(context.Background(), RegisterRequest{InterfaceID: "x", Major: 1}); err != nil {
		t.Fatalf("RegisterEndpoint: %v", err)
	}

	// 服务端投一个 Dispatch。用 setHandler 的下一次调用注入，触发器是
	// UnregisterEndpoint 这次往返。
	f.setHandler(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if dr := env.GetDispatchResult(); dr != nil {
			results <- dr
			return nil
		}
		if u := env.GetUnregisterEndpoint(); u != nil {
			return []*ipcv1.Envelope{
				{Body: &ipcv1.Envelope_UnregisterEndpointResult{
					UnregisterEndpointResult: &ipcv1.UnregisterEndpointResult{
						RequestId: u.GetRequestId(),
						Outcome: &ipcv1.UnregisterEndpointResult_Success{
							Success: &ipcv1.UnregisterEndpointSuccess{},
						},
					},
				}},
				{Body: &ipcv1.Envelope_Dispatch{Dispatch: &ipcv1.Dispatch{
					RouteId:     77,
					EndpointId:  1,
					MethodId:    5,
					RemainingMs: 2000,
					Payload:     []byte("hi"),
					Caller: &ipcv1.CallerContext{
						PackageId:          "com.caller",
						Uid:                20001,
						TrustProfile:       ipcv1.TrustProfile_TRUST_PROFILE_ORDINARY,
						GrantedPermissions: []string{"perm.motion.control"},
					},
				}}},
			}
		}
		return nil
	})

	if err := h.UnregisterEndpoint(context.Background(), 1, false); err != nil {
		t.Fatalf("UnregisterEndpoint: %v", err)
	}

	select {
	case dr := <-results:
		if dr.GetRouteId() != 77 {
			t.Errorf("route_id = %d, want 77 (must echo, not invent)", dr.GetRouteId())
		}
		s := dr.GetSuccess()
		if s == nil {
			t.Fatalf("outcome = %v, want success", dr.GetOutcome())
		}
		if string(s.GetPayload()) != "ok:hi" {
			t.Errorf("payload = %q", s.GetPayload())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no DispatchResult")
	}

	// CallerContext 必须原样交给 handler：Service 靠它做二次 fail-closed 复核
	if gotCaller.GetPackageId() != "com.caller" || gotCaller.GetUid() != 20001 {
		t.Errorf("caller = %+v", gotCaller)
	}
	if len(gotCaller.GetGrantedPermissions()) != 1 {
		t.Errorf("granted permissions not passed through: %+v", gotCaller.GetGrantedPermissions())
	}
}

func TestServiceHost_UnknownMethodFailsClosed(t *testing.T) {
	// 没有 handler 就是没实现。静默成功会让调用方以为动作执行了——
	// 对机器人来说那意味着「以为狗停了，其实没停」。
	dr := dispatchViaUnregister(t, nil, &ipcv1.Dispatch{
		RouteId: 5, MethodId: 12345, RemainingMs: 1000,
	})
	f := dr.GetFailure()
	if f == nil {
		t.Fatalf("outcome = %v, want failure", dr.GetOutcome())
	}
	if f.GetCode() != ipcv1.StatusCode_STATUS_CODE_NOT_FOUND {
		t.Errorf("code = %v, want NOT_FOUND", f.GetCode())
	}
}

func TestServiceHost_HandlerErrorNormalizedToInternal(t *testing.T) {
	// 任意 Go error 的字符串不得越过进程边界：那会泄漏路径、依赖、栈信息。
	dr := dispatchViaUnregister(t, func(h *ServiceHost) {
		h.Handle(1, func(cc CallContext, p []byte) ([]byte, error) {
			return nil, errors.New("internal detail: /etc/secret not found")
		})
	}, &ipcv1.Dispatch{RouteId: 6, MethodId: 1, RemainingMs: 1000})

	f := dr.GetFailure()
	if f == nil || f.GetCode() != ipcv1.StatusCode_STATUS_CODE_INTERNAL {
		t.Fatalf("failure = %v, want INTERNAL", f)
	}
	if contains(f.GetPublicMessage(), "secret") {
		t.Fatalf("internal detail leaked to wire: %q", f.GetPublicMessage())
	}
}

func TestServiceHost_StatusErrorPassesCodeAndDetail(t *testing.T) {
	detail := mustMarshal(t, &ipcv1.ControlLeaseErrorDetail{
		Reason: ipcv1.ControlLeaseErrorReason_CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN,
	})
	dr := dispatchViaUnregister(t, func(h *ServiceHost) {
		h.Handle(1, func(cc CallContext, p []byte) ([]byte, error) {
			return nil, &StatusError{
				Code:          ipcv1.StatusCode_STATUS_CODE_FAILED_PRECONDITION,
				PublicMessage: "should not reach the wire",
				Detail:        detail,
			}
		})
	}, &ipcv1.Dispatch{RouteId: 7, MethodId: 1, RemainingMs: 1000})

	f := dr.GetFailure()
	if f == nil || f.GetCode() != ipcv1.StatusCode_STATUS_CODE_FAILED_PRECONDITION {
		t.Fatalf("failure = %v", f)
	}
	// typed detail 必须原样带上——它是调用方区分细因的唯一渠道
	if len(f.GetErrorDetail()) == 0 {
		t.Fatal("error_detail dropped")
	}
	// 但 public_message【不能】由 Provider 写：协议规定它只能由 nervud 从
	// 受审计模板生成，禁止透传 Provider 自由文本
	if f.GetPublicMessage() != "" {
		t.Errorf("provider must not set public_message, got %q", f.GetPublicMessage())
	}
}

func TestServiceHost_HandlerPanicBecomesInternal(t *testing.T) {
	// 一个 handler panic 不该带走整个 Service 进程：那会让同进程其它 endpoint
	// 的在途调用一起消失，nervud 侧只看到连接断开，无法归因到具体方法。
	dr := dispatchViaUnregister(t, func(h *ServiceHost) {
		h.Handle(1, func(cc CallContext, p []byte) ([]byte, error) {
			panic("boom")
		})
	}, &ipcv1.Dispatch{RouteId: 8, MethodId: 1, RemainingMs: 1000})

	f := dr.GetFailure()
	if f == nil || f.GetCode() != ipcv1.StatusCode_STATUS_CODE_INTERNAL {
		t.Fatalf("panic should become INTERNAL, got %v", f)
	}
}

// dispatchViaUnregister 是这些用例的共用夹具：起 host、注册 handler、报到，
// 然后借一次 UnregisterEndpoint 往返让服务端把 Dispatch 推过来，收 DispatchResult。
//
// 借往返而不是让 fakeNervud 主动推，是因为测试替身的 handler 是「收到才回」的
// 请求-响应形态，没有独立的推送时机。真 nervud 是随时可推的，这个差异不影响
// 被测逻辑（ServiceHost 的读循环对 Dispatch 何时到达没有假设）。
func dispatchViaUnregister(t *testing.T, register func(*ServiceHost), d *ipcv1.Dispatch) *ipcv1.DispatchResult {
	t.Helper()

	results := make(chan *ipcv1.DispatchResult, 1)
	f := startFakeNervud(t)
	f.setHandler(registerThen(nil))

	h, err := NewServiceHost(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("NewServiceHost: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })

	if register != nil {
		register(h)
	}
	if _, err := h.RegisterEndpoint(context.Background(), RegisterRequest{InterfaceID: "x", Major: 1}); err != nil {
		t.Fatalf("RegisterEndpoint: %v", err)
	}

	f.setHandler(func(c net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if dr := env.GetDispatchResult(); dr != nil {
			select {
			case results <- dr:
			default:
			}
			return nil
		}
		if u := env.GetUnregisterEndpoint(); u != nil {
			return []*ipcv1.Envelope{
				{Body: &ipcv1.Envelope_UnregisterEndpointResult{
					UnregisterEndpointResult: &ipcv1.UnregisterEndpointResult{
						RequestId: u.GetRequestId(),
						Outcome: &ipcv1.UnregisterEndpointResult_Success{
							Success: &ipcv1.UnregisterEndpointSuccess{},
						},
					},
				}},
				{Body: &ipcv1.Envelope_Dispatch{Dispatch: d}},
			}
		}
		return nil
	})

	if err := h.UnregisterEndpoint(context.Background(), 1, false); err != nil {
		t.Fatalf("UnregisterEndpoint: %v", err)
	}

	select {
	case dr := <-results:
		return dr
	case <-time.After(5 * time.Second):
		t.Fatal("no DispatchResult received")
		return nil
	}
}
