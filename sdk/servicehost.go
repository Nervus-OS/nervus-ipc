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

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
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
	// RouteID 是 nervud 为本次 Dispatch 分配的连接作用域路由号。业务代码不得
	// 把它当身份或权限凭据；它只用于调用通用 Transfer Control 时填写
	// origin_route_id。
	RouteID uint64
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

	// Execution 是 nervud 在方法门禁后冻结的可信执行快照。需要控制租约的方法
	// 必须在真正触碰设备前使用其中的 resource generation、motion epoch、命令
	// 序号和绝对 deadline 做最后一道陈旧检查。LeaseId 只用于关联，不能当作可
	// 转让的 bearer capability。
	Execution *ipcv1.ExecutionContext
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

type handlerKey struct {
	endpointID uint64
	methodID   uint32
}

type controlWaiter struct {
	ch            chan *ipcv1.Envelope
	beforeDeliver func(*ipcv1.Envelope)
}

type inflightCall struct {
	endpointID uint64
	cancel     context.CancelFunc
}

// ServiceHost 是控制面的提供侧连接。
type ServiceHost struct {
	co  *conn
	ids *requestIDGen
	log *slog.Logger

	mu sync.RWMutex
	// handlers 必须以 endpoint_id + method_id 联合寻址。不同接口的 method_id
	// 都从 1 开始，只按 method_id 建表会让后注册的接口覆盖先注册的接口。
	handlers map[handlerKey]Handler
	// legacyHandlers 是 Handle + RegisterEndpoint 兼容 API 的模板。每次报到时
	// 都会做快照并安装到该次返回的 endpoint_id 下。
	legacyHandlers map[uint32]Handler

	// pending 登记这条 Service 连接主动发出的控制请求与普通 Request。
	pendingMu sync.Mutex
	pending   map[uint64]*controlWaiter

	inflightMu sync.Mutex
	inflight   map[uint64]*inflightCall

	// subs 是订阅状态机，见 subscribe.go。Service 也会订阅别人的事件——
	// nervus.camerad 就要订阅厂商 Provider 的帧就绪事件。
	subs *subscriber

	readErr  error
	readDone chan struct{}
}

// writeEnvelope / nextRequestID 让 ServiceHost 满足 eventTransport
// （closedErr 已有）。
func (h *ServiceHost) writeEnvelope(env *ipcv1.Envelope) error { return h.co.writeEnvelope(env) }
func (h *ServiceHost) nextRequestID() (uint64, error)          { return h.ids.nextID() }

// Subscribe 订阅一个 endpoint 上的事件。语义与 Client.Subscribe 完全相同。
func (h *ServiceHost) Subscribe(ctx context.Context, req SubscribeRequest) (*Subscription, error) {
	return h.subs.subscribe(ctx, req)
}

// Unsubscribe 撤下一条订阅并关闭它的事件通道。
func (h *ServiceHost) Unsubscribe(ctx context.Context, sub *Subscription) error {
	return h.subs.unsubscribe(ctx, sub)
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
	// v2 起 Dispatch 无条件携带 ExecutionContext，不再有 minor 门。曾经那道
	// 检查存在的理由（1.0 的 Dispatch 没有调用方身份，Provider 无法鉴权）已经
	// 随 major 一起消失：能握上手就说明对端是 v2。

	h := &ServiceHost{
		co:             co,
		ids:            newRequestIDGen(),
		log:            cfg.Log,
		handlers:       make(map[handlerKey]Handler),
		legacyHandlers: make(map[uint32]Handler),
		pending:        make(map[uint64]*controlWaiter),
		inflight:       make(map[uint64]*inflightCall),
		readDone:       make(chan struct{}),
	}
	h.subs = newSubscriber(h, cfg.Log)
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

// Handle 注册兼容模式的 method_id 处理函数。
//
// RegisterEndpoint 会在每次报到时快照这些函数，并把它们绑定到新 endpoint。
// 多个接口存在重号 method_id 时应改用 NewEndpoint；未注册的方法以 NOT_FOUND
// 回复——fail closed。
func (h *ServiceHost) Handle(methodID uint32, fn Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.legacyHandlers[methodID] = fn
}

// Close 关闭连接。幂等。
func (h *ServiceHost) Close() error {
	h.co.close()
	<-h.readDone
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

// EndpointHost 把一组 handler 与一次 endpoint 报到绑定在一起。
//
// method_id 只在一个接口 major 内唯一，因此一个 ServiceHost 承载多个接口时，
// 必须使用 EndpointHost，不能把所有 handler 塞进一张全局 method_id 表。
type EndpointHost struct {
	host *ServiceHost
	req  RegisterRequest

	mu         sync.Mutex
	handlers   map[uint32]Handler
	endpointID uint64
	registered bool
}

// NewEndpoint 创建一个尚未报到的 endpoint。返回值不建立新连接；同一进程里的
// 所有 endpoint 仍复用 ServiceHost 的一条通用 IPC 连接。
func (h *ServiceHost) NewEndpoint(req RegisterRequest) *EndpointHost {
	req.SchemaHash = append([]byte(nil), req.SchemaHash...)
	return &EndpointHost{
		host:     h,
		req:      req,
		handlers: make(map[uint32]Handler),
	}
}

// Handle 为这个 endpoint 注册一个方法。必须在 Register 之前完成。
func (e *EndpointHost) Handle(methodID uint32, fn Handler) error {
	if methodID == 0 {
		return errors.New("sdk: method id 0 is reserved")
	}
	if fn == nil {
		return errors.New("sdk: nil handler")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.registered {
		return errors.New("sdk: cannot change handlers while endpoint is registered")
	}
	e.handlers[methodID] = fn
	return nil
}

// Register 原子地报到 endpoint 并安装其 handler。
//
// RegisterEndpointResult 成功与后续 Dispatch 可能紧邻到达；ServiceHost 会在读
// 循环交付成功结果前先安装 handler，因而不存在“报到成功但第一帧找不到方法”的
// 窗口。
func (e *EndpointHost) Register(ctx context.Context) (uint64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.registered {
		return 0, errors.New("sdk: endpoint is already registered")
	}
	handlers := cloneHandlers(e.handlers)
	id, err := e.host.registerEndpoint(ctx, e.req, handlers)
	if err != nil {
		return 0, err
	}
	e.endpointID = id
	e.registered = true
	return id, nil
}

// EndpointID 返回当前已报到句柄；未报到时 ok=false。
func (e *EndpointHost) EndpointID() (id uint64, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.endpointID, e.registered
}

// Unregister 撤下 endpoint。成功后可以再次 Register，原 handler 保留。
func (e *EndpointHost) Unregister(ctx context.Context, drain bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.registered {
		return errors.New("sdk: endpoint is not registered")
	}
	if err := e.host.UnregisterEndpoint(ctx, e.endpointID, drain); err != nil {
		return err
	}
	e.endpointID = 0
	e.registered = false
	return nil
}

// PublishEvent 在本 endpoint 上报一条事件，不带产生时刻。
//
// 适用于纯信号与状态变更（「流开了」「设备掉了」）——这类事件的意义在于
// 「发生了」，收到的先后就是全部时序信息。
//
// 承载采样数据的事件应当用 PublishEventAt 带上采集时刻。
func (e *EndpointHost) PublishEvent(eventID uint32, payload []byte) error {
	return e.PublishEventAt(eventID, payload, 0)
}

// PublishEventAt 上报一条带产生时刻的事件。
//
// # 时间戳必须由调用方给出，本 SDK 不代劳
//
// 有意义的时刻是【数据产生的那一刻】，而不是「Go 代码腾出手来发布的那一刻」。
// 摄像头帧的正确取值是 V4L2 buffer 自带的 CLOCK_MONOTONIC 时间戳，IMU 是采样
// 中断的时刻——这些都在驱动那一层，SDK 拿不到。让 SDK 随手打一个
// time.Now() 会产出一个看起来合理、实际上混进了排队延迟的数字，而消费者
// 无法分辨它是不是真的采集时刻。
//
// 拿不到真实采集时刻就传 0（表示未提供），不要用发布时刻冒充。
//
// # 单向，没有结果
//
// 上报不等结果：给它配一个结果会让 Provider 的事件循环变成请求-响应，
// 一个慢订阅者就能拖住整个 Provider。背压的正确落点在 nervud 与订阅方之间。
//
// 返回的 error 只表示【本地写失败】（连接已断）。事件被 nervud 拒绝
// （event_id 不在契约里、超出 max_payload_bytes）不会有任何回音，只在内核
// 审计里留一条 ipc.PublishEventRejected。
func (e *EndpointHost) PublishEventAt(eventID uint32, payload []byte, monotonicNanos uint64) error {
	e.mu.Lock()
	id, registered := e.endpointID, e.registered
	e.mu.Unlock()
	if !registered {
		return errors.New("sdk: endpoint is not registered")
	}
	return e.host.co.writeEnvelope(&ipcv1.Envelope{
		Body: &ipcv1.Envelope_PublishEvent{PublishEvent: &ipcv1.PublishEvent{
			EndpointId:              id,
			EventId:                 eventID,
			Payload:                 payload,
			MonotonicTimestampNanos: monotonicNanos,
		}},
	})
}

// ResolveEndpoint 在同一条 Service 控制连接上解析一个消费侧 endpoint。
//
// Provider 需要回调系统接口（尤其 Transfer Control）时必须用这个方法，而不是
// 另开 Client 连接：Dispatch.route_id 只在收到该 Dispatch 的连接上有意义。
func (h *ServiceHost) ResolveEndpoint(ctx context.Context, req ResolveRequest) (Endpoint, error) {
	id, err := h.ids.nextID()
	if err != nil {
		return Endpoint{}, err
	}
	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_ResolveEndpoint{
		ResolveEndpoint: &ipcv1.ResolveEndpoint{
			RequestId:         id,
			InterfaceId:       req.InterfaceID,
			MinInterfaceMajor: req.MinMajor,
			MaxInterfaceMajor: req.MaxMajor,
			Selector:          req.selector(),
		},
	}}
	res, err := h.roundTrip(ctx, id, env, nil)
	if err != nil {
		return Endpoint{}, err
	}
	r := res.GetResolveEndpointResult()
	if r == nil {
		return Endpoint{}, fmt.Errorf("%w: expected ResolveEndpointResult", ErrProtocol)
	}
	if failure := r.GetFailure(); failure != nil {
		return Endpoint{}, statusErrorFrom(failure)
	}
	success := r.GetSuccess()
	if success == nil || success.GetEndpointId() == 0 {
		return Endpoint{}, fmt.Errorf("%w: ResolveEndpoint success has invalid endpoint id", ErrProtocol)
	}
	return Endpoint{
		EndpointID:     success.GetEndpointId(),
		Major:          success.GetInterfaceMajor(),
		Minor:          success.GetInterfaceMinor(),
		SchemaHash:     append([]byte(nil), success.GetInterfaceSchemaHash()...),
		ResourceHandle: success.GetResourceHandle(),
	}, nil
}

// Call 从同一条 Service 控制连接发起普通 Request。
//
// 该方法与 Client.Call 使用相同 wire；区别只是 reader 由 ServiceHost 统一管理，
// 因而 handler 可以安全地在处理 Dispatch 时回调内核系统接口，不会与服务侧
// Dispatch reader 争抢同一个 socket。
func (h *ServiceHost) Call(
	ctx context.Context,
	endpointID uint64,
	methodID uint32,
	payload []byte,
	timeout time.Duration,
) ([]byte, error) {
	id, err := h.ids.nextID()
	if err != nil {
		return nil, err
	}
	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_Request{Request: &ipcv1.Request{
		RequestId:  id,
		EndpointId: endpointID,
		MethodId:   methodID,
		TimeoutMs:  uint32(timeout.Milliseconds()),
		Payload:    payload,
	}}}
	res, err := h.roundTrip(ctx, id, env, nil)
	if err != nil {
		return nil, err
	}
	response := res.GetResponse()
	if response == nil {
		return nil, fmt.Errorf("%w: expected Response", ErrProtocol)
	}
	if failure := response.GetFailure(); failure != nil {
		return nil, statusErrorFrom(failure)
	}
	success := response.GetSuccess()
	if success == nil {
		return nil, fmt.Errorf("%w: Response has neither outcome", ErrProtocol)
	}
	return append([]byte(nil), success.GetPayload()...), nil
}

// RegisterEndpoint 向 nervud 报到。
//
// 这是【报到，不是自行创造权限】：只能注册 manifest 已声明、且与签名/权限
// profile 相符的 endpoint。nervud 独立裁决，声明什么不等于拿到什么。
//
// 返回的 endpointID 是【Service 侧句柄】，与调用方的 endpoint_id 是两个命名
// 空间：同一个数字在 App 连接和本连接上毫无关系。
func (h *ServiceHost) RegisterEndpoint(ctx context.Context, req RegisterRequest) (uint64, error) {
	h.mu.RLock()
	handlers := cloneHandlers(h.legacyHandlers)
	h.mu.RUnlock()
	return h.registerEndpoint(ctx, req, handlers)
}

func (h *ServiceHost) registerEndpoint(
	ctx context.Context,
	req RegisterRequest,
	handlers map[uint32]Handler,
) (uint64, error) {
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

	res, err := h.roundTrip(ctx, id, env, func(res *ipcv1.Envelope) {
		r := res.GetRegisterEndpointResult()
		if r == nil {
			return
		}
		if success := r.GetSuccess(); success != nil && success.GetEndpointId() != 0 {
			h.installEndpointHandlers(success.GetEndpointId(), handlers)
		}
	})
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
	success := r.GetSuccess()
	if success == nil || success.GetEndpointId() == 0 {
		return 0, fmt.Errorf("%w: RegisterEndpoint success has invalid endpoint id", ErrProtocol)
	}
	return success.GetEndpointId(), nil
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
	res, err := h.roundTrip(ctx, id, env, func(res *ipcv1.Envelope) {
		r := res.GetUnregisterEndpointResult()
		if r != nil && r.GetSuccess() != nil {
			h.invalidateEndpoint(endpointID)
		}
	})
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

// roundTrip 发一个带 request_id 的控制请求或普通 Request，并等它的结果
// Envelope。Dispatch 仍由读循环独立驱动。
func (h *ServiceHost) roundTrip(
	ctx context.Context,
	id uint64,
	env *ipcv1.Envelope,
	beforeDeliver func(*ipcv1.Envelope),
) (*ipcv1.Envelope, error) {
	ch := make(chan *ipcv1.Envelope, 1)
	h.pendingMu.Lock()
	h.pending[id] = &controlWaiter{ch: ch, beforeDeliver: beforeDeliver}
	h.pendingMu.Unlock()

	drop := func() bool {
		h.pendingMu.Lock()
		_, present := h.pending[id]
		if present {
			delete(h.pending, id)
		}
		h.pendingMu.Unlock()
		return present
	}

	if err := h.co.writeEnvelope(env); err != nil {
		drop()
		return nil, err
	}
	select {
	case <-ctx.Done():
		if drop() {
			return nil, ctx.Err()
		}
		// deliverControl 已经从 pending 取走 waiter，说明权威结果正在交付。
		// 等它写入容量 1 的 channel，避免 Register 成功已安装 handler、调用方
		// 却因 select 竞态只看到 context canceled 的撕裂状态。
		res, ok := <-ch
		if !ok {
			return nil, h.closedErr()
		}
		return res, nil
	case res, ok := <-ch:
		if !ok {
			return nil, h.closedErr()
		}
		return res, nil
	}
}

func cloneHandlers(src map[uint32]Handler) map[uint32]Handler {
	out := make(map[uint32]Handler, len(src))
	for methodID, handler := range src {
		out[methodID] = handler
	}
	return out
}

func (h *ServiceHost) installEndpointHandlers(endpointID uint64, handlers map[uint32]Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for methodID, handler := range handlers {
		h.handlers[handlerKey{endpointID: endpointID, methodID: methodID}] = handler
	}
}

func (h *ServiceHost) invalidateEndpoint(endpointID uint64) {
	h.mu.Lock()
	for key := range h.handlers {
		if key.endpointID == endpointID {
			delete(h.handlers, key)
		}
	}
	h.mu.Unlock()

	h.inflightMu.Lock()
	var calls []*inflightCall
	for _, call := range h.inflight {
		if call.endpointID == endpointID {
			calls = append(calls, call)
		}
	}
	h.inflightMu.Unlock()
	for _, call := range calls {
		call.cancel()
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
	// 订阅相关的四个 body 在两种角色上语义相同，交给共用的状态机。
	if h.subs.handleSubscriptionBody(env) {
		return false
	}

	switch body := env.GetBody().(type) {
	case *ipcv1.Envelope_Dispatch:
		// startDispatch 在读循环里先登记 route，再启动 goroutine。这样紧跟在
		// Dispatch 后到达的 CancelDispatch 不会抢在登记之前丢失。
		if !h.startDispatch(body.Dispatch) {
			h.readErr = fmt.Errorf("%w: duplicate or invalid dispatch route", ErrProtocol)
			return true
		}
		return false

	case *ipcv1.Envelope_CancelDispatch:
		h.cancelDispatch(body.CancelDispatch.GetRouteId())
		return false

	case *ipcv1.Envelope_RegisterEndpointResult:
		h.deliverControl(body.RegisterEndpointResult.GetRequestId(), env)
		return false

	case *ipcv1.Envelope_UnregisterEndpointResult:
		h.deliverControl(body.UnregisterEndpointResult.GetRequestId(), env)
		return false

	case *ipcv1.Envelope_ResolveEndpointResult:
		h.deliverControl(body.ResolveEndpointResult.GetRequestId(), env)
		return false

	case *ipcv1.Envelope_Response:
		h.deliverControl(body.Response.GetRequestId(), env)
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

	case *ipcv1.Envelope_EndpointRevoked:
		h.invalidateEndpoint(body.EndpointRevoked.GetEndpointId())
		h.log.Info("sdk: endpoint invalidated by server",
			"endpoint_id", body.EndpointRevoked.GetEndpointId(), "body", "EndpointRevoked")
		return false

	case *ipcv1.Envelope_EndpointDied:
		h.invalidateEndpoint(body.EndpointDied.GetEndpointId())
		h.log.Info("sdk: endpoint invalidated by server",
			"endpoint_id", body.EndpointDied.GetEndpointId(), "body", "EndpointDied")
		return false

	default:
		h.readErr = fmt.Errorf("%w: unexpected body %T on service connection", ErrProtocol, body)
		h.log.Warn("sdk: unexpected body, closing", "body", fmt.Sprintf("%T", body))
		return true
	}
}

func (h *ServiceHost) deliverControl(id uint64, env *ipcv1.Envelope) {
	h.pendingMu.Lock()
	waiter, ok := h.pending[id]
	if ok {
		delete(h.pending, id)
	}
	h.pendingMu.Unlock()
	if !ok {
		h.log.Debug("sdk: dropping unmatched control result", "request_id", id)
		return
	}
	if waiter.beforeDeliver != nil {
		waiter.beforeDeliver(env)
	}
	waiter.ch <- env
}

func (h *ServiceHost) startDispatch(d *ipcv1.Dispatch) bool {
	if d == nil || d.GetRouteId() == 0 || !validExecutionContext(d.GetExecutionContext()) {
		return false
	}
	// remaining_ms 是剩余预算。为 0 时不设 deadline——协议里 0 表示「用默认值」
	// 而不是「已经超时」，把它当 0 秒 deadline 会让每个这样的调用立刻失败。
	ctx := context.Background()
	var cancel context.CancelFunc
	if ms := d.GetRemainingMs(); ms > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	call := &inflightCall{endpointID: d.GetEndpointId(), cancel: cancel}

	h.inflightMu.Lock()
	if _, duplicate := h.inflight[d.GetRouteId()]; duplicate {
		h.inflightMu.Unlock()
		cancel()
		return false
	}
	h.inflight[d.GetRouteId()] = call
	h.inflightMu.Unlock()

	// 一个慢方法不该挡住同一 Service 上的其它调用。并发上限由 nervud 的
	// max_inflight_requests 在源头控制，这里不再叠加第二层队列。
	go h.dispatch(d, ctx, call)
	return true
}

func (h *ServiceHost) cancelDispatch(routeID uint64) {
	h.inflightMu.Lock()
	call := h.inflight[routeID]
	h.inflightMu.Unlock()
	if call == nil {
		h.log.Debug("sdk: dropping cancel for unknown route", "route_id", routeID)
		return
	}
	call.cancel()
}

func (h *ServiceHost) finishDispatch(routeID uint64, call *inflightCall) {
	h.inflightMu.Lock()
	if h.inflight[routeID] == call {
		delete(h.inflight, routeID)
	}
	h.inflightMu.Unlock()
	call.cancel()
}

// dispatch 执行一次方法调用并回 DispatchResult。
func (h *ServiceHost) dispatch(d *ipcv1.Dispatch, ctx context.Context, call *inflightCall) {
	routeID := d.GetRouteId()
	defer h.finishDispatch(routeID, call)

	h.mu.RLock()
	fn, ok := h.handlers[handlerKey{endpointID: d.GetEndpointId(), methodID: d.GetMethodId()}]
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
		RouteID:    routeID,
		EndpointID: d.GetEndpointId(),
		Caller:     d.GetCaller(),
		Execution:  d.GetExecutionContext(),
	}

	payload, err := h.safeCall(fn, cc, d.GetPayload())
	if err != nil {
		var se *StatusError
		if errors.As(err, &se) {
			h.replyFailure(routeID, se)
			return
		}
		if errors.Is(err, context.Canceled) {
			h.replyFailure(routeID, &StatusError{Code: ipcv1.StatusCode_STATUS_CODE_CANCELLED})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			h.replyFailure(routeID, &StatusError{Code: ipcv1.StatusCode_STATUS_CODE_DEADLINE_EXCEEDED})
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

func validExecutionContext(ec *ipcv1.ExecutionContext) bool {
	if ec == nil || ec.GetDeadlineNanos() <= 0 {
		return false
	}
	hasResource := ec.GetResourceHandle() != ""
	if hasResource != (ec.GetResourceGeneration() != 0) {
		return false
	}
	if ec.GetLeaseId() == 0 {
		return ec.GetControllerClass() == ipcv1.ControllerClass_CONTROLLER_CLASS_UNSPECIFIED &&
			ec.GetMotionEpoch() == 0 && ec.GetCommandSequence() == 0
	}
	if !hasResource || ec.GetMotionEpoch() == 0 || ec.GetCommandSequence() == 0 {
		return false
	}
	switch ec.GetControllerClass() {
	case ipcv1.ControllerClass_CONTROLLER_CLASS_HUMAN,
		ipcv1.ControllerClass_CONTROLLER_CLASS_AI:
		return true
	default:
		return false
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
	h.subs.closeAll(h.closedErr())

	h.pendingMu.Lock()
	m := h.pending
	h.pending = make(map[uint64]*controlWaiter)
	h.pendingMu.Unlock()
	for _, waiter := range m {
		close(waiter.ch)
	}

	h.inflightMu.Lock()
	calls := make([]*inflightCall, 0, len(h.inflight))
	for _, call := range h.inflight {
		calls = append(calls, call)
	}
	h.inflight = make(map[uint64]*inflightCall)
	h.inflightMu.Unlock()
	for _, call := range calls {
		call.cancel()
	}
}

func (h *ServiceHost) closedErr() error {
	if h.readErr != nil {
		return fmt.Errorf("%w: %v", ErrClosed, h.readErr)
	}
	return ErrClosed
}
