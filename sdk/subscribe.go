// 本文件是订阅侧：消费方的 Subscribe/Unsubscribe/Event，以及提供方的 PublishEvent。
//
// # 三条连接的关系
//
//	订阅方 --Subscribe(40)--> nervud            建立 (endpoint, event_id) 订阅
//	Provider --PublishEvent(53)--> nervud       上报一条事件
//	nervud --Event(43)--> 每个订阅方            扇出，各自的 subscription_id 与 sequence
//
// Provider 【看不到】订阅者，也不该看到：订阅方的权限可能在订阅之后被撤销，
// Provider 无从得知。它只说「这个 endpoint 上发生了这个事件」，投递决策留在
// nervud。这就是为什么提供侧只有 PublishEvent 而没有「发给某某」的 API。
//
// # 背压在本文件里出现两次
//
// 第一次在 nervud 与本进程之间（内核按 delivery_class 合并/丢弃/断订阅），
// 第二次在本文件的读循环与调用方的 range 之间。两处的规则【刻意一致】——
// 读循环绝不能阻塞在往 Subscription 的 channel 里塞事件上，否则一个慢消费者
// 会连带饿死这条连接上所有的请求响应与 Ping。
package sdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// DefaultSubscriptionBuffer 是单条订阅的默认本地缓冲深度。
//
// 取 64 而不是 1：读循环是整条连接共用的，缓冲太浅会让一次普通的 GC 暂停就
// 造成可见丢弃。也不取更大——缓冲的作用是吸收抖动，不是替调用方囤积数据；
// 真需要囤的调用方应当自己在 range 里落盘或转投更大的队列。
const DefaultSubscriptionBuffer = 64

var (
	// ErrSubscriptionOverflow 本地缓冲满，且该订阅是 RELIABLE。
	//
	// RELIABLE 不允许静默丢弃，所以这里【终止订阅】而不是丢一条——与 nervud
	// 在同样处境下断掉慢订阅者是同一条规则，只是发生在下一跳。
	ErrSubscriptionOverflow = errors.New("sdk: reliable subscription overflowed local buffer")

	// ErrSubscriptionClosed 订阅已由服务端终止，原因见 *SubscriptionClosedError。
	ErrSubscriptionClosed = errors.New("sdk: subscription closed by server")
)

// SubscriptionClosedError 携带服务端给出的终止原因。
type SubscriptionClosedError struct {
	Reason ipcv1.SubscriptionClosedReason
}

func (e *SubscriptionClosedError) Error() string {
	return fmt.Sprintf("%s: %s", ErrSubscriptionClosed.Error(), e.Reason)
}

func (e *SubscriptionClosedError) Unwrap() error { return ErrSubscriptionClosed }

// Event 是一条收到的推送事件。
type Event struct {
	SubscriptionID uint64
	// Sequence 本订阅内单调递增，从 1 开始。缺口 = 有事件没送到。
	Sequence uint64
	// EndpointID 事件来源 endpoint（订阅方视角的句柄）。
	EndpointID uint64
	EventID    uint32
	// Payload 事件专属 Protobuf bytes，类型由 (endpoint, event_id) 决定。
	Payload []byte

	// Dropped 是自上一条【你实际收到的】事件以来丢失的条数。
	//
	// 它把 nervud 侧的丢弃与本 SDK 缓冲的丢弃【合并计数】。合并是刻意的：
	// 对调用方来说两者是同一件事——「你没看到的事件有这么多条」，而该怎么办
	// 完全由 DeliveryClass 决定，与丢在哪一跳无关。
	//
	// 要分辨丢在哪一跳（排查用）看 Subscription.LocalDropped()。
	Dropped uint64

	// MonotonicTimestampNanos 是 Provider 给出的产生时刻，原样透传。0 表示未提供。
	// 【不是收到时刻】：事件经过 nervud 扇出与两级队列，两者可能差很多。
	MonotonicTimestampNanos uint64
}

// Subscription 是一条已建立的订阅。
//
// 用法：
//
//	sub, err := c.Subscribe(ctx, sdk.SubscribeRequest{EndpointID: ep.EndpointID, EventID: 2})
//	defer c.Unsubscribe(context.Background(), sub)
//	for ev := range sub.C() {
//	    ...
//	}
//	if err := sub.Err(); err != nil { /* 非正常结束 */ }
type Subscription struct {
	id         uint64
	endpointID uint64
	eventID    uint32
	class      ipcv1.DeliveryClass

	ch chan Event

	mu sync.Mutex
	// pendingDropped 是还没能搭上任何一条已投递事件的本地丢弃数。
	// 投递成功时清零并计入那条事件的 Dropped。
	pendingDropped uint64
	localDropped   uint64
	closed         bool
	err            error
}

// ID 返回本连接作用域的订阅句柄。
func (s *Subscription) ID() uint64 { return s.id }

// EndpointID 返回事件来源 endpoint。
func (s *Subscription) EndpointID() uint64 { return s.endpointID }

// EventID 返回订阅的事件 ID。
func (s *Subscription) EventID() uint32 { return s.eventID }

// DeliveryClass 返回 nervud 声明的投递类别。
//
// 【必须据它解释 Dropped】：STATE 下 Dropped>0 只意味着中间态被合并，当前这条
// 就是最新值，无需补拉；LOSSY 下则是永久丢失。同一个数字，两种相反的处置。
func (s *Subscription) DeliveryClass() ipcv1.DeliveryClass { return s.class }

// C 是事件通道。订阅结束时被关闭，因此可以直接 range。
func (s *Subscription) C() <-chan Event { return s.ch }

// Err 给出订阅结束的原因。正常退订返回 nil。
//
// 只在 C() 已关闭后有意义——在那之前恒为 nil。
func (s *Subscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// LocalDropped 是本 SDK 缓冲丢弃的累计条数（不含 nervud 侧丢弃）。
//
// 与 Event.Dropped 分开只为排查：它非 0 说明【本进程消费太慢】，该加大
// SubscribeRequest.Buffer 或让 handler 更快，而不是去查内核。
func (s *Subscription) LocalDropped() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localDropped
}

// deliver 由读循环调用，返回非 nil 表示该订阅必须终止。
//
// 【绝不阻塞】：读循环是整条连接共用的。阻塞在这里会让同一连接上的请求响应、
// Ping、乃至其它订阅一起饿死——一个慢消费者拖垮全连接，正是 delivery_class
// 机制要防的事。
func (s *Subscription) deliver(ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}

	// carried 是「你上次实际收到之后丢了多少」的本地部分。它必须搭上某一条
	// 真正送达的事件才算报出去了——所以只在 trySend 成功后才清零。
	carried := s.pendingDropped
	if s.trySend(ev, carried) {
		s.pendingDropped = 0
		return nil
	}

	switch s.class {
	case ipcv1.DeliveryClass_DELIVERY_CLASS_STATE:
		// 合并：丢掉队列里【最旧】的一条，把最新的挤进去。STATE 的语义是
		// 「只要最新值」，丢新留旧正好把语义反过来。
		//
		// 被挤掉的那条要计进本条的 Dropped，否则合并就成了静默丢弃——
		// 调用方看到 sequence 跳号却拿不到条数，正是 Dropped 存在的理由。
		if s.evictOldest() {
			carried++
			if s.trySend(ev, carried) {
				s.pendingDropped = 0
				return nil
			}
		}
		s.localDropped++
		s.pendingDropped = carried + 1
		return nil

	case ipcv1.DeliveryClass_DELIVERY_CLASS_LOSSY:
		s.localDropped++
		s.pendingDropped = carried + 1
		return nil

	default:
		// RELIABLE，以及任何本 build 不认识的类别。
		//
		// 【未知类别按 RELIABLE 处理】：协议新增一个值时，把它当成「可以随便丢」
		// 是最危险的默认——调用方会以为自己拿到了完整序列。宁可终止订阅。
		return ErrSubscriptionOverflow
	}
}

// trySend 非阻塞投递，dropped 是要搭在本条上的本地丢弃数。
func (s *Subscription) trySend(ev Event, dropped uint64) bool {
	ev.Dropped += dropped
	select {
	case s.ch <- ev:
		return true
	default:
		return false
	}
}

// evictOldest 挤掉队首最旧的一条，返回是否确实挤掉了。
func (s *Subscription) evictOldest() bool {
	select {
	case <-s.ch:
		s.localDropped++
		return true
	default:
		return false
	}
}

// closeWith 关闭订阅并记下原因。幂等。
func (s *Subscription) closeWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.err = err
	close(s.ch)
}

// SubscribeRequest 是一次订阅请求。
type SubscribeRequest struct {
	// EndpointID 必须是本连接上已 Resolve 的 endpoint 句柄。
	EndpointID uint64
	// EventID 接口 schema 中稳定的数字事件 ID。
	EventID uint32
	// Payload 订阅参数（过滤条件、采样率等）的序列化 bytes，可为空。
	// 类型由 (endpoint, event_id) 决定。
	Payload []byte
	// Buffer 本地事件缓冲深度。<=0 取 DefaultSubscriptionBuffer。
	Buffer int
}

// eventTransport 是订阅实现对宿主连接的窄依赖。Client 与 ServiceHost 都满足。
//
// 定义在消费者这一侧（本文件）而不是让两个宿主各写一份订阅逻辑：订阅的状态机
// 与两种角色无关，重复实现只会让两边慢慢长歪。
type eventTransport interface {
	writeEnvelope(*ipcv1.Envelope) error
	nextRequestID() (uint64, error)
	closedErr() error
}

// pendingSubscribe 是一次在途 Subscribe：订阅对象已经建好，只等 nervud 给出
// subscription_id 与 delivery_class。
//
// 【订阅必须在读循环里登记，不能等调用方 goroutine 醒来】。nervud 完全可能在
// 发出 SubscribeResult 之后紧接着就推第一条事件——两者在同一条字节流上，中间
// 没有任何间隙。若登记发生在调用方拿到结果之后，那几条事件会落进「未知
// subscription_id」被丢弃，而且丢得毫无痕迹：调用方只会看到 sequence 不是从 1
// 开始，还以为是自己订晚了。
type pendingSubscribe struct {
	sub *Subscription
	ch  chan *ipcv1.Envelope
}

// subscriber 是订阅状态机，Client 与 ServiceHost 各持有一个。
type subscriber struct {
	tr  eventTransport
	log *slog.Logger

	mu sync.Mutex
	// subs 按 subscription_id 索引。键由 nervud 分配且【同连接内永不复用】，
	// 所以不需要额外的代次字段来分辨新旧订阅。
	subs map[uint64]*Subscription
	// pendingSubs 登记在途的 Subscribe，按 request_id 索引。
	pendingSubs map[uint64]*pendingSubscribe
	// ctl 登记在途的 Unsubscribe，按 request_id 索引。
	ctl    map[uint64]chan *ipcv1.Envelope
	closed bool
}

func newSubscriber(tr eventTransport, log *slog.Logger) *subscriber {
	return &subscriber{
		tr:          tr,
		log:         log,
		subs:        make(map[uint64]*Subscription),
		pendingSubs: make(map[uint64]*pendingSubscribe),
		ctl:         make(map[uint64]chan *ipcv1.Envelope),
	}
}

// subscribe 发起一次订阅并等待结果。
func (b *subscriber) subscribe(ctx context.Context, req SubscribeRequest) (*Subscription, error) {
	if req.EndpointID == 0 {
		return nil, errors.New("sdk: SubscribeRequest.EndpointID is required")
	}
	buffer := req.Buffer
	if buffer <= 0 {
		buffer = DefaultSubscriptionBuffer
	}

	id, err := b.tr.nextRequestID()
	if err != nil {
		return nil, err
	}
	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_Subscribe{Subscribe: &ipcv1.Subscribe{
		RequestId:  id,
		EndpointId: req.EndpointID,
		EventId:    req.EventID,
		Payload:    req.Payload,
	}}}

	// 订阅对象在【发出请求之前】就建好，这样读循环拿到 SubscribeResult 时
	// 可以就地登记，紧随其后的事件不会漏。见 pendingSubscribe 的注释。
	pending := &pendingSubscribe{
		sub: &Subscription{
			endpointID: req.EndpointID,
			eventID:    req.EventID,
			ch:         make(chan Event, buffer),
		},
		ch: make(chan *ipcv1.Envelope, 1),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, b.tr.closedErr()
	}
	b.pendingSubs[id] = pending
	b.mu.Unlock()

	if err := b.tr.writeEnvelope(env); err != nil {
		b.mu.Lock()
		delete(b.pendingSubs, id)
		b.mu.Unlock()
		return nil, err
	}

	var res *ipcv1.Envelope
	select {
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pendingSubs, id)
		b.mu.Unlock()
		return nil, ctx.Err()
	case env, ok := <-pending.ch:
		if !ok {
			return nil, b.tr.closedErr()
		}
		res = env
	}

	result := res.GetSubscribeResult()
	if f := result.GetFailure(); f != nil {
		return nil, statusErrorFrom(f)
	}
	success := result.GetSuccess()
	if success == nil {
		return nil, fmt.Errorf("%w: SubscribeResult has neither outcome", ErrProtocol)
	}
	if success.GetSubscriptionId() == 0 {
		// 0 是保留值。接受它会让后续所有 Event 匹配不上任何订阅。
		return nil, fmt.Errorf("%w: SubscribeSuccess carries subscription_id 0", ErrProtocol)
	}
	return pending.sub, nil
}

// finishSubscribe 在【读循环里】完成一次订阅的登记，然后把结果交给等待者。
//
// 登记与投递在同一次持锁内完成，因此读循环处理下一帧（可能就是第一条事件）
// 时，订阅一定已经在表里了。
func (b *subscriber) finishSubscribe(requestID uint64, env *ipcv1.Envelope) bool {
	result := env.GetSubscribeResult()

	b.mu.Lock()
	pending, ok := b.pendingSubs[requestID]
	if !ok {
		b.mu.Unlock()
		return false
	}
	delete(b.pendingSubs, requestID)

	if success := result.GetSuccess(); success != nil && success.GetSubscriptionId() != 0 {
		pending.sub.id = success.GetSubscriptionId()
		pending.sub.class = success.GetDeliveryClass()
		b.subs[pending.sub.id] = pending.sub
	}
	b.mu.Unlock()

	pending.ch <- env
	return true
}

// unsubscribe 撤下一条订阅。
//
// 【收到 UnsubscribeResult 之后才摘表】：连接上的字节是有序的，因此结果之前
// 到达的 Event 都还属于这条订阅，结果之后不会再有。提前摘表会让那些在途事件
// 变成「未知 subscription_id」而被丢弃并记一条噪音日志。
func (b *subscriber) unsubscribe(ctx context.Context, sub *Subscription) error {
	if sub == nil {
		return errors.New("sdk: nil subscription")
	}
	id, err := b.tr.nextRequestID()
	if err != nil {
		return err
	}
	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_Unsubscribe{Unsubscribe: &ipcv1.Unsubscribe{
		RequestId:      id,
		SubscriptionId: sub.id,
	}}}

	res, err := b.roundTrip(ctx, id, env)
	if err != nil {
		return err
	}
	result := res.GetUnsubscribeResult()
	if result == nil {
		return fmt.Errorf("%w: expected UnsubscribeResult, got %T", ErrProtocol, res.GetBody())
	}

	b.mu.Lock()
	delete(b.subs, sub.id)
	b.mu.Unlock()
	// 正常退订：err 留 nil，调用方的 range 自然结束。
	sub.closeWith(nil)

	if f := result.GetFailure(); f != nil {
		return statusErrorFrom(f)
	}
	return nil
}

// roundTrip 发出一个控制请求并等待同 request_id 的结果。
func (b *subscriber) roundTrip(
	ctx context.Context, id uint64, env *ipcv1.Envelope,
) (*ipcv1.Envelope, error) {
	ch := make(chan *ipcv1.Envelope, 1)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, b.tr.closedErr()
	}
	b.ctl[id] = ch
	b.mu.Unlock()

	if err := b.tr.writeEnvelope(env); err != nil {
		b.mu.Lock()
		delete(b.ctl, id)
		b.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.ctl, id)
		b.mu.Unlock()
		return nil, ctx.Err()
	case res, ok := <-ch:
		if !ok {
			return nil, b.tr.closedErr()
		}
		return res, nil
	}
}

// deliverControl 把 Subscribe/UnsubscribeResult 交给等待者。返回是否命中。
//
// 未命中【不关连接】：一个已超时走人的订阅请求收到迟到结果是正常时序，
// 与 Response 的处理同规。
func (b *subscriber) deliverControl(id uint64, env *ipcv1.Envelope) bool {
	b.mu.Lock()
	ch, ok := b.ctl[id]
	if ok {
		delete(b.ctl, id)
	}
	b.mu.Unlock()
	if !ok {
		return false
	}
	ch <- env
	return true
}

// deliverEvent 把一条 Event 投给对应订阅。
//
// 未知 subscription_id 丢弃并记日志，不关连接：退订与在途事件之间总有窗口。
func (b *subscriber) deliverEvent(e *ipcv1.Event) {
	b.mu.Lock()
	sub, ok := b.subs[e.GetSubscriptionId()]
	b.mu.Unlock()
	if !ok {
		b.log.Debug("sdk: dropping event for unknown subscription",
			"subscription_id", e.GetSubscriptionId(), "event_id", e.GetEventId())
		return
	}

	err := sub.deliver(Event{
		SubscriptionID:          e.GetSubscriptionId(),
		Sequence:                e.GetSequence(),
		EndpointID:              e.GetEndpointId(),
		EventID:                 e.GetEventId(),
		Payload:                 e.GetPayload(),
		Dropped:                 e.GetDropped(),
		MonotonicTimestampNanos: e.GetMonotonicTimestampNanos(),
	})
	if err == nil {
		return
	}

	// RELIABLE 溢出。只终止这一条订阅，【不关连接】——同一条连接上可能还有
	// 别的、消费得动的订阅，为一个慢消费者拆掉全部是过度反应。
	b.mu.Lock()
	delete(b.subs, sub.id)
	b.mu.Unlock()
	sub.closeWith(err)
	b.log.Warn("sdk: reliable subscription terminated by local backpressure",
		"subscription_id", sub.id, "event_id", sub.eventID)
}

// deliverClosed 处理服务端的 SubscriptionClosed(45)。
func (b *subscriber) deliverClosed(c *ipcv1.SubscriptionClosed) {
	b.mu.Lock()
	sub, ok := b.subs[c.GetSubscriptionId()]
	if ok {
		delete(b.subs, c.GetSubscriptionId())
	}
	b.mu.Unlock()
	if !ok {
		b.log.Debug("sdk: SubscriptionClosed for unknown subscription",
			"subscription_id", c.GetSubscriptionId())
		return
	}
	sub.closeWith(&SubscriptionClosedError{Reason: c.GetReason()})
}

// closeAll 在连接断开时终止全部订阅与在途控制请求。
func (b *subscriber) closeAll(err error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := b.subs
	ctl := b.ctl
	pending := b.pendingSubs
	b.subs = make(map[uint64]*Subscription)
	b.ctl = make(map[uint64]chan *ipcv1.Envelope)
	b.pendingSubs = make(map[uint64]*pendingSubscribe)
	b.mu.Unlock()

	for _, sub := range subs {
		sub.closeWith(err)
	}
	// 关闭而不是投递错误值：等待方用「收到 nil」区分「连接没了」与
	// 「收到了真正的结果」，与 pendingMap.failAll 同规。
	for _, ch := range ctl {
		close(ch)
	}
	for _, p := range pending {
		close(p.ch)
	}
}

// handleSubscriptionBody 分派订阅相关的四个 body，返回是否已处理。
//
// Client 与 ServiceHost 的读循环共用它——订阅在两种角色上语义完全相同，
// 各写一份只会让某一侧慢慢漏掉一个分支。
func (b *subscriber) handleSubscriptionBody(env *ipcv1.Envelope) bool {
	switch body := env.GetBody().(type) {
	case *ipcv1.Envelope_SubscribeResult:
		if !b.finishSubscribe(body.SubscribeResult.GetRequestId(), env) {
			b.log.Debug("sdk: dropping unmatched SubscribeResult",
				"request_id", body.SubscribeResult.GetRequestId())
		}
		return true

	case *ipcv1.Envelope_UnsubscribeResult:
		if !b.deliverControl(body.UnsubscribeResult.GetRequestId(), env) {
			b.log.Debug("sdk: dropping unmatched UnsubscribeResult",
				"request_id", body.UnsubscribeResult.GetRequestId())
		}
		return true

	case *ipcv1.Envelope_Event:
		b.deliverEvent(body.Event)
		return true

	case *ipcv1.Envelope_SubscriptionClosed:
		b.deliverClosed(body.SubscriptionClosed)
		return true

	default:
		return false
	}
}
