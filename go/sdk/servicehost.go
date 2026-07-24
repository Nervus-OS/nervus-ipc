// 本文件是提供侧 ServiceHost：向 nervud 报到（RegisterEndpoint），然后循环接收
// nervud 转发来的 Dispatch、执行、回 DispatchResult。
//
// # 它不是服务器
//
// Service 永远不持有客户端 socket，也无权决定结果写给谁。它只认识 nervud 给的
// route_id，那个数字在 nervud 内部映射到 (来源连接, 来源 request_id, deadline,
// binding generation, method schema)——这些【都不上 wire】，Service 无法伪造
// 它不知道的东西。这正是不能直接复用客户端 request_id 做全局路由的原因。
//
// 典型用法：
//
//	h, err := sdk.NewServiceHost(sdk.Config{ComponentID: "main"})
//	h.Handle(methodSetVelocity, func(ctx sdk.CallContext, payload []byte) ([]byte, error) { ... })
//	ep, err := h.RegisterEndpoint(ctx, sdk.RegisterRequest{...})
//	h.Serve()   // 阻塞
package sdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ipcv1 "github.com/nervus-os/nervus-ipc/go/protocol/ipcv1"
)

// CallContext 是一次 Dispatch 的上下文，交给 handler。
type CallContext struct {
	// Ctx 已按 Dispatch.remaining_ms 设好 deadline。handler 必须尊重它——
	// remaining_ms 是本次调用【剩余】的预算（排队转发已经消耗了一部分），
	// 不是原始 timeout。算超了 nervud 早已回了 DEADLINE_EXCEEDED，你的结果
	// 会被当作迟到结果丢弃。
	Ctx context.Context

	// MethodID 被调用的方法。
	MethodID uint32
	// EndpointID 本 Service 侧的 endpoint 句柄（RegisterEndpoint 拿到的那个）。
	EndpointID uint64

	// Caller 是 nervud 附加的【可信】调用者上下文，来自内核 SO_PEERCRED，
	// 不是调用方自填。
	//
	// 可以读，但【不能】用它绕过已经生效的 nervud Policy，更不能自行创造身份
	// 或权限裁决。GrantedPermissions 是 nervud 裁决结果的只读投影，供你做二次
	// fail-closed 复核（「我这个方法要 X 权限，投影里没有 X，拒绝」），不是让你
	// 重新裁决。
	Caller *ipcv1.CallerContext
}

// Handler 处理一次方法调用。
//
// 返回 (payload, nil) 表示成功；返回 error 表示失败：
//   - *StatusError 会原样映射成 wire 上的 code 与 error_detail
//   - 其它 error 一律归一化为 INTERNAL，原文只进本地日志
//
// 后者是刻意的：把任意 Go error 的字符串塞给调用方等于把内部实现细节
// （路径、SQL、栈信息）泄漏出进程边界。要给调用方可区分的原因，就显式构造
// *StatusError 并带上该接口自己的 typed error_detail。
type Handler func(cc CallContext, payload []byte) ([]byte, error)

// ServiceHost 是控制面的提供侧连接。
type ServiceHost struct {
	co  *conn
	ids *requestIDGen
	log *slog.Logger

	mu       sync.RWMutex
	handlers map[uint32]Handler

	// registers 登记在途的 RegisterEndpoint / UnregisterEndpoint 请求。
	regMu     sync.Mutex
	registers map[uint64]chan *ipcv1.Envelope

	readErr  error
	readDone chan struct{}
}

// NewServiceHost 建立连接并完成握手。
func NewServiceHost(cfg Config) (*ServiceHost, error) {
	cfg.applyDefaults()
	if cfg.ComponentID == "" {
		return nil, errors.New("sdk: Config.ComponentID is required")
	}

	co, err := dial(cfg.SockPath, cfg.DialTimeout)
	if err != nil {
		return nil, err
	}
	if err := co.handshake(cfg.SDKName, cfg.SDKVersion, cfg.ComponentID, cfg.HandshakeTimeout); err != nil {
		co.close()
		return nil, err
	}

	h := &ServiceHost{
		co:        co,
		ids:       newRequestIDGen(),
		log:       cfg.Log,
		handlers:  make(map[uint32]Handler),
		registers: make(map[uint64]chan *ipcv1.Envelope),
		readDone:  make(chan struct{}),
	}
	// 读循环在构造时就起，不等 Serve：RegisterEndpoint 需要读它自己的响应，
	// 而调用顺序必然是「先报到、再 Serve」。若把读循环放进 Serve，报到时就
	// 没有 reader，RegisterEndpoint 永远等不到结果——死锁。
	go h.serve()
	return h, nil
}

// Identity 返回握手时核对确认的身份。
func (h *ServiceHost) Identity() Identity { return h.co.Identity() }

// Limits 返回本连接生效的预算。
func (h *ServiceHost) Limits() *ipcv1.ConnectionLimits { return h.co.Limits() }

// Handle 注册一个 method_id 的处理函数。必须在 Serve 之前调用完。
//
// 未注册的 method_id 会以 NOT_FOUND 回复——fail closed。绝不静默成功：
// 那会让调用方以为动作执行了。
func (h *ServiceHost) Handle(methodID uint32, fn Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[methodID] = fn
}

// Close 关闭连接。幂等。
func (h *ServiceHost) Close() error {
	h.co.close()
	return nil
}

// RegisterRequest 是一次 endpoint 报到。
type RegisterRequest struct {
	InterfaceID  string
	Major, Minor uint32
	// SchemaHash 本 Service 实现的接口 schema hash。与 nervud 登记值不符即被拒——
	// 这道校验是为了避免一个用旧 schema 编译的 Provider 悄悄服务新接口。
	SchemaHash []byte
	// ResourceHandle 该 endpoint 服务的 Resource，如 "base.main"。
	ResourceHandle string
}

// RegisterEndpoint 向 nervud 报到。
//
// 这是【报到，不是自行创造权限】：只能注册 manifest 已声明、且与签名/权限
// profile 相符的 endpoint。nervud 独立裁决，声明什么不等于拿到什么。
//
// 返回的 endpointID 是【Service 侧句柄】，与调用方的 endpoint_id 是两个命名
// 空间：同一个数字在 App 连接和本连接上毫无关系。
func (h *ServiceHost) RegisterEndpoint(ctx context.Context, req RegisterRequest) (uint64, error) {
	id, err := h.ids.nextID()
	if err != nil {
		return 0, err
	}
	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_RegisterEndpoint{
		RegisterEndpoint: &ipcv1.RegisterEndpoint{
			RequestId:           id,
			InterfaceId:         req.InterfaceID,
			InterfaceMajor:      req.Major,
			InterfaceMinor:      req.Minor,
			InterfaceSchemaHash: req.SchemaHash,
			ResourceHandle:      req.ResourceHandle,
		},
	}}

	res, err := h.roundTrip(ctx, id, env)
	if err != nil {
		return 0, err
	}
	r := res.GetRegisterEndpointResult()
	if r == nil {
		return 0, fmt.Errorf("%w: expected RegisterEndpointResult", ErrProtocol)
	}
	if f := r.GetFailure(); f != nil {
		return 0, statusErrorFrom(f)
	}
	return r.GetSuccess().GetEndpointId(), nil
}

// UnregisterEndpoint 优雅撤下一个 endpoint（NRCP §16.2 的 SHUTTING_DOWN）。
//
// drain=true 表示等在途 Dispatch 完成再撤。
//
// 【注意】nervud 当前未接线 drain（执行层没有在途 Dispatch 追踪），drain=true
// 会退化成立即撤，在途调用以 UNAVAILABLE 终结。
func (h *ServiceHost) UnregisterEndpoint(ctx context.Context, endpointID uint64, drain bool) error {
	id, err := h.ids.nextID()
	if err != nil {
		return err
	}
	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_UnregisterEndpoint{
		UnregisterEndpoint: &ipcv1.UnregisterEndpoint{
			RequestId: id, EndpointId: endpointID, Drain: drain,
		},
	}}
	res, err := h.roundTrip(ctx, id, env)
	if err != nil {
		return err
	}
	r := res.GetUnregisterEndpointResult()
	if r == nil {
		return fmt.Errorf("%w: expected UnregisterEndpointResult", ErrProtocol)
	}
	if f := r.GetFailure(); f != nil {
		return statusErrorFrom(f)
	}
	return nil
}

// roundTrip 发一个带 request_id 的控制请求并等它的结果 Envelope。
//
// 只用于 Register/Unregister 这类低频控制往返，不用于 Dispatch——后者由 Serve
// 的读循环驱动。
func (h *ServiceHost) roundTrip(ctx context.Context, id uint64, env *ipcv1.Envelope) (*ipcv1.Envelope, error) {
	ch := make(chan *ipcv1.Envelope, 1)
	h.regMu.Lock()
	h.registers[id] = ch
	h.regMu.Unlock()

	drop := func() {
		h.regMu.Lock()
		delete(h.registers, id)
		h.regMu.Unlock()
	}

	if err := h.co.writeEnvelope(env); err != nil {
		drop()
		return nil, err
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case res, ok := <-ch:
		if !ok {
			return nil, h.closedErr()
		}
		return res, nil
	}
}

// Serve 阻塞直到连接断开，返回断开原因（本端 Close 导致的返回 nil）。
//
// 读循环在 NewServiceHost 时就已启动，Serve 只是等它结束。因此推荐的调用顺序
// 是：Handle 注册全部处理函数 → RegisterEndpoint 报到 → Serve 阻塞。
//
// 【务必在报到前把 Handle 调完】。报到成功那一刻 nervud 就可能转发 Dispatch，
// 此时还没注册的 method 会被回 NOT_FOUND——调用方会以为这个方法不存在，而不是
// 「服务还没准备好」，这种错误很难往回追。
func (h *ServiceHost) Serve() error {
	<-h.readDone
	return h.readErr
}

func (h *ServiceHost) serve() {
	defer close(h.readDone)
	defer h.failAllPending()

	for {
		env, err := h.co.readEnvelope()
		if err != nil {
			select {
			case <-h.co.closed:
				// 本端主动 Close 导致的读失败，不是异常
			default:
				h.readErr = err
			}
			h.co.close()
			return
		}
		if stop := h.handleEnvelope(env); stop {
			h.co.close()
			return
		}
	}
}

// handleEnvelope 分派一个收到的 Envelope，返回是否应当终止连接。
func (h *ServiceHost) handleEnvelope(env *ipcv1.Envelope) bool {
	switch body := env.GetBody().(type) {
	case *ipcv1.Envelope_Dispatch:
		// 每个 Dispatch 起一个 goroutine：一个慢方法不该挡住同一 Service 上的
		// 其它调用。并发上限由 nervud 的 max_inflight_requests 在源头控制，
		// 这里不再叠加一层队列——两处限流会让「到底卡在哪」变得难以诊断。
		go h.dispatch(body.Dispatch)
		return false

	case *ipcv1.Envelope_RegisterEndpointResult:
		h.deliverControl(body.RegisterEndpointResult.GetRequestId(), env)
		return false

	case *ipcv1.Envelope_UnregisterEndpointResult:
		h.deliverControl(body.UnregisterEndpointResult.GetRequestId(), env)
		return false

	case *ipcv1.Envelope_Ping:
		pong := &ipcv1.Envelope{Body: &ipcv1.Envelope_Pong{
			Pong: &ipcv1.Pong{Nonce: body.Ping.GetNonce()},
		}}
		if err := h.co.writeEnvelope(pong); err != nil {
			h.readErr = err
			return true
		}
		return false

	case *ipcv1.Envelope_Pong:
		return false

	case *ipcv1.Envelope_EndpointRevoked, *ipcv1.Envelope_EndpointDied:
		h.log.Info("sdk: endpoint invalidated by server", "body", fmt.Sprintf("%T", body))
		return false

	default:
		// CancelDispatch(52) 也落在这里：内核当前不会发它（上游 Cancel 未实现，
		// 没有触发源）。真收到说明内核已接通而 SDK 没跟上——按协议违规关闭，
		// 让问题立刻暴露，而不是静默忽略一个「本该停下来」的信号。
		h.readErr = fmt.Errorf("%w: unexpected body %T on service connection", ErrProtocol, body)
		h.log.Warn("sdk: unexpected body, closing", "body", fmt.Sprintf("%T", body))
		return true
	}
}

func (h *ServiceHost) deliverControl(id uint64, env *ipcv1.Envelope) {
	h.regMu.Lock()
	ch, ok := h.registers[id]
	if ok {
		delete(h.registers, id)
	}
	h.regMu.Unlock()
	if !ok {
		h.log.Debug("sdk: dropping unmatched control result", "request_id", id)
		return
	}
	ch <- env
}

// dispatch 执行一次方法调用并回 DispatchResult。
func (h *ServiceHost) dispatch(d *ipcv1.Dispatch) {
	routeID := d.GetRouteId()

	// remaining_ms 是剩余预算。为 0 时不设 deadline——协议里 0 表示「用默认值」
	// 而不是「已经超时」，把它当 0 秒 deadline 会让每个这样的调用立刻失败。
	ctx := context.Background()
	var cancel context.CancelFunc
	if ms := d.GetRemainingMs(); ms > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
		defer cancel()
	}

	h.mu.RLock()
	fn, ok := h.handlers[d.GetMethodId()]
	h.mu.RUnlock()

	if !ok {
		// fail closed：没有 handler 就是没实现，绝不静默成功——静默成功会让
		// 调用方以为动作执行了。原因只进本地日志（public_message 不由 Provider 写）
		h.log.Warn("sdk: no handler for method", "method_id", d.GetMethodId(), "route_id", routeID)
		h.replyFailure(routeID, &StatusError{Code: ipcv1.StatusCode_STATUS_CODE_NOT_FOUND})
		return
	}

	cc := CallContext{
		Ctx:        ctx,
		MethodID:   d.GetMethodId(),
		EndpointID: d.GetEndpointId(),
		Caller:     d.GetCaller(),
	}

	payload, err := h.safeCall(fn, cc, d.GetPayload())
	if err != nil {
		var se *StatusError
		if errors.As(err, &se) {
			h.replyFailure(routeID, se)
			return
		}
		// 非 StatusError 一律归一化为 INTERNAL，原文只进本地日志——不把内部
		// 实现细节（路径/栈/依赖错误）泄漏出进程边界。
		h.log.Error("sdk: handler failed", "method_id", d.GetMethodId(), "err", err)
		h.replyFailure(routeID, &StatusError{Code: ipcv1.StatusCode_STATUS_CODE_INTERNAL})
		return
	}

	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_DispatchResult{
		DispatchResult: &ipcv1.DispatchResult{
			RouteId: routeID,
			Outcome: &ipcv1.DispatchResult_Success{Success: &ipcv1.Success{
				Code:    ipcv1.StatusCode_STATUS_CODE_OK,
				Payload: payload,
			}},
		},
	}}
	if err := h.co.writeEnvelope(env); err != nil {
		h.log.Debug("sdk: send DispatchResult failed", "route_id", routeID, "err", err)
	}
}

// safeCall 执行 handler 并把 panic 转成 error。
//
// 一个 Provider 的 handler panic 不该带走整个 Service 进程：那会让同一进程里
// 其它 endpoint 的在途调用一起消失，nervud 侧只看到连接断开，无法归因到具体
// 方法。转成 INTERNAL 让调用方拿到明确失败，日志里留下完整现场。
func (h *ServiceHost) safeCall(fn Handler, cc CallContext, payload []byte) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("sdk: handler panicked", "method_id", cc.MethodID, "panic", r)
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return fn(cc, payload)
}

// replyFailure 回一个失败的 DispatchResult。
//
// 【刻意不下发 public_message】：status.proto 规定它只能由 nervud 从受审计模板
// 和已脱敏的结构化字段生成，禁止原样透传 Provider 的自由文本。Provider 想表达
// 可区分的细因，唯一正当渠道是 error_detail（该接口自己的 typed detail）——
// nervud 会按权威 schema 解码校验、丢弃未知字段后重新编码再转发。
// StatusError.PublicMessage 在提供侧因此被忽略，只用于本地日志。
func (h *ServiceHost) replyFailure(routeID uint64, se *StatusError) {
	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_DispatchResult{
		DispatchResult: &ipcv1.DispatchResult{
			RouteId: routeID,
			Outcome: &ipcv1.DispatchResult_Failure{Failure: &ipcv1.Failure{
				Code:        se.Code,
				ErrorDetail: se.Detail,
			}},
		},
	}}
	if err := h.co.writeEnvelope(env); err != nil {
		h.log.Debug("sdk: send failure DispatchResult failed", "route_id", routeID, "err", err)
	}
}

func (h *ServiceHost) failAllPending() {
	h.regMu.Lock()
	m := h.registers
	h.registers = make(map[uint64]chan *ipcv1.Envelope)
	h.regMu.Unlock()
	for _, ch := range m {
		close(ch)
	}
}

func (h *ServiceHost) closedErr() error {
	if h.readErr != nil {
		return fmt.Errorf("%w: %v", ErrClosed, h.readErr)
	}
	return ErrClosed
}
