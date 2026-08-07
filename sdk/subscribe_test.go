package sdk

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// ---- 背压策略（对 Subscription.deliver 的单元测试） ------------------------
//
// 走本地方法而不是走 wire：三种 delivery_class 的差别全在「缓冲满的那一刻」，
// 而那一刻在真实连接上无法确定地复现。策略要有确定性覆盖，wire 另有 e2e。

func newTestSub(class ipcv1.DeliveryClass, buffer int) *Subscription {
	return &Subscription{id: 1, endpointID: 7, eventID: 3, class: class, ch: make(chan Event, buffer)}
}

func deliverN(t *testing.T, s *Subscription, from, to uint64) error {
	t.Helper()
	for seq := from; seq <= to; seq++ {
		if err := s.deliver(Event{SubscriptionID: s.id, Sequence: seq}); err != nil {
			return err
		}
	}
	return nil
}

// RELIABLE 缓冲满时【终止订阅】而不是丢一条。
//
// 丢弃会让调用方以为自己拿到了完整序列——RELIABLE 承诺的正是「没有这种可能」。
func TestSubscription_ReliableOverflowTerminates(t *testing.T) {
	s := newTestSub(ipcv1.DeliveryClass_DELIVERY_CLASS_RELIABLE, 2)

	if err := deliverN(t, s, 1, 2); err != nil {
		t.Fatalf("前两条应当装得下: %v", err)
	}
	err := s.deliver(Event{Sequence: 3})
	if !errors.Is(err, ErrSubscriptionOverflow) {
		t.Fatalf("第三条 err = %v, want ErrSubscriptionOverflow", err)
	}
}

// LOSSY 丢新的，并把丢弃条数搭在【下一条送达的】事件上。
func TestSubscription_LossyFoldsDropsIntoNextDelivered(t *testing.T) {
	s := newTestSub(ipcv1.DeliveryClass_DELIVERY_CLASS_LOSSY, 1)

	// 1 装进缓冲；2、3 丢弃
	if err := deliverN(t, s, 1, 3); err != nil {
		t.Fatalf("LOSSY 不应终止订阅: %v", err)
	}
	first := <-s.C()
	if first.Sequence != 1 {
		t.Fatalf("first.Sequence = %d, want 1（丢的应当是新的，不是队首）", first.Sequence)
	}
	if first.Dropped != 0 {
		t.Errorf("first.Dropped = %d, want 0：丢弃发生在它之后", first.Dropped)
	}

	// 缓冲腾空后，积压的两条丢弃必须搭上下一条
	if err := s.deliver(Event{Sequence: 4}); err != nil {
		t.Fatalf("deliver 4: %v", err)
	}
	next := <-s.C()
	if next.Sequence != 4 {
		t.Fatalf("next.Sequence = %d, want 4", next.Sequence)
	}
	if next.Dropped != 2 {
		t.Errorf("next.Dropped = %d, want 2（seq 2、3）", next.Dropped)
	}
	if got := s.LocalDropped(); got != 2 {
		t.Errorf("LocalDropped = %d, want 2", got)
	}
}

// STATE 挤掉【最旧】的一条，留下最新值，并把被挤掉的计进本条 Dropped。
//
// 丢新留旧会把 STATE 的语义整个反过来：调用方拿到的将是一个越来越陈旧的值。
func TestSubscription_StateEvictsOldestAndCounts(t *testing.T) {
	s := newTestSub(ipcv1.DeliveryClass_DELIVERY_CLASS_STATE, 2)

	if err := deliverN(t, s, 1, 4); err != nil {
		t.Fatalf("STATE 不应终止订阅: %v", err)
	}

	// 缓冲 2，投了 4 条：留下的必须是最新的两条
	got := []Event{<-s.C(), <-s.C()}
	if got[0].Sequence != 3 || got[1].Sequence != 4 {
		t.Fatalf("留下的是 seq %d,%d，want 3,4（最新值）", got[0].Sequence, got[1].Sequence)
	}
	// seq 1 与 2 各被挤掉一次，分别计进挤它出去的那一条
	if got[0].Dropped != 1 || got[1].Dropped != 1 {
		t.Errorf("Dropped = %d,%d, want 1,1", got[0].Dropped, got[1].Dropped)
	}
	if n := s.LocalDropped(); n != 2 {
		t.Errorf("LocalDropped = %d, want 2", n)
	}
}

// 【本 build 不认识的 delivery_class 按 RELIABLE 处理】。
//
// 协议将来新增一档时，把未知值当成「可以随便丢」是最危险的默认。
func TestSubscription_UnknownClassFailsClosedToReliable(t *testing.T) {
	s := newTestSub(ipcv1.DeliveryClass(99), 1)
	if err := s.deliver(Event{Sequence: 1}); err != nil {
		t.Fatalf("第一条: %v", err)
	}
	if err := s.deliver(Event{Sequence: 2}); !errors.Is(err, ErrSubscriptionOverflow) {
		t.Fatalf("err = %v, want ErrSubscriptionOverflow", err)
	}
}

// ---- wire 往返 -------------------------------------------------------------

func subscribeOK(subID uint64, class ipcv1.DeliveryClass) func(net.Conn, *ipcv1.Envelope) []*ipcv1.Envelope {
	return autoHandshake(func(_ net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		s := env.GetSubscribe()
		if s == nil {
			return nil
		}
		return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_SubscribeResult{
			SubscribeResult: &ipcv1.SubscribeResult{
				RequestId: s.GetRequestId(),
				Outcome: &ipcv1.SubscribeResult_Success{Success: &ipcv1.SubscribeSuccess{
					SubscriptionId: subID, DeliveryClass: class,
				}},
			},
		}}}
	})
}

func eventEnv(subID, seq uint64, payload []byte) *ipcv1.Envelope {
	return &ipcv1.Envelope{Body: &ipcv1.Envelope_Event{Event: &ipcv1.Event{
		SubscriptionId: subID, Sequence: seq, EndpointId: 7, EventId: 3, Payload: payload,
	}}}
}

func mustSubscribe(t *testing.T, c *Client, buffer int) *Subscription {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sub, err := c.Subscribe(ctx, SubscribeRequest{EndpointID: 7, EventID: 3, Buffer: buffer})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return sub
}

// 订阅成功必须把 delivery_class 带给调用方。
//
// 没有它，调用方看到 sequence 缺口时无从判断该「什么都不做」还是
// 「数据永久丢失」——同一个现象，两种相反的处置。
func TestSubscribe_ExposesDeliveryClass(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(subscribeOK(11, ipcv1.DeliveryClass_DELIVERY_CLASS_STATE))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	sub := mustSubscribe(t, c, 0)
	if sub.ID() != 11 {
		t.Errorf("ID = %d, want 11", sub.ID())
	}
	if sub.DeliveryClass() != ipcv1.DeliveryClass_DELIVERY_CLASS_STATE {
		t.Errorf("DeliveryClass = %v, want STATE", sub.DeliveryClass())
	}
}

// subscription_id 为 0 是保留值，接受它会让后续所有 Event 匹配不上任何订阅。
func TestSubscribe_ZeroSubscriptionIDIsProtocolViolation(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(subscribeOK(0, ipcv1.DeliveryClass_DELIVERY_CLASS_RELIABLE))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Subscribe(ctx, SubscribeRequest{EndpointID: 7, EventID: 3}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want ErrProtocol", err)
	}
}

func TestSubscribe_FailureBecomesStatusError(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(_ net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		s := env.GetSubscribe()
		if s == nil {
			return nil
		}
		return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_SubscribeResult{
			SubscribeResult: &ipcv1.SubscribeResult{
				RequestId: s.GetRequestId(),
				Outcome: &ipcv1.SubscribeResult_Failure{Failure: &ipcv1.Failure{
					Code: ipcv1.StatusCode_STATUS_CODE_PERMISSION_DENIED,
				}},
			},
		}}}
	}))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = c.Subscribe(ctx, SubscribeRequest{EndpointID: 7, EventID: 3})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != ipcv1.StatusCode_STATUS_CODE_PERMISSION_DENIED {
		t.Fatalf("err = %v, want PERMISSION_DENIED StatusError", err)
	}
}

// Event(43) 必须按 subscription_id 路由到对应订阅，原样保留 sequence 与 payload。
func TestSubscribe_EventsRouteToSubscription(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(_ net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		s := env.GetSubscribe()
		if s == nil {
			return nil
		}
		// 订阅结果与随后的事件在同一批回发：连接上的字节有序，
		// 因此客户端登记订阅一定发生在事件到达之前。
		return []*ipcv1.Envelope{
			{Body: &ipcv1.Envelope_SubscribeResult{SubscribeResult: &ipcv1.SubscribeResult{
				RequestId: s.GetRequestId(),
				Outcome: &ipcv1.SubscribeResult_Success{Success: &ipcv1.SubscribeSuccess{
					SubscriptionId: 11,
					DeliveryClass:  ipcv1.DeliveryClass_DELIVERY_CLASS_RELIABLE,
				}},
			}}},
			eventEnv(11, 1, []byte("a")),
			eventEnv(11, 2, []byte("b")),
		}
	}))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	sub := mustSubscribe(t, c, 0)
	for i, want := range []string{"a", "b"} {
		select {
		case ev := <-sub.C():
			if string(ev.Payload) != want {
				t.Fatalf("第 %d 条 payload = %q, want %q", i+1, ev.Payload, want)
			}
			if ev.Sequence != uint64(i+1) {
				t.Fatalf("第 %d 条 sequence = %d", i+1, ev.Sequence)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("第 %d 条事件超时", i+1)
		}
	}
}

// 未知 subscription_id 的 Event 只丢弃，【不关连接】。
//
// 退订与在途事件之间总有窗口，把它当协议违规会让正常时序频繁踢掉连接。
func TestSubscribe_UnknownSubscriptionEventDoesNotKillConn(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(_ net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if env.GetResolveEndpoint() == nil {
			return nil
		}
		return []*ipcv1.Envelope{
			eventEnv(999, 1, nil),
			{Body: &ipcv1.Envelope_ResolveEndpointResult{
				ResolveEndpointResult: &ipcv1.ResolveEndpointResult{
					RequestId: env.GetResolveEndpoint().GetRequestId(),
					Outcome: &ipcv1.ResolveEndpointResult_Success{
						Success: &ipcv1.ResolveEndpointSuccess{EndpointId: 7, InterfaceMajor: 1},
					},
				},
			}},
		}
	}))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// 幽灵事件之后紧跟的 Resolve 仍然要能拿到结果——连接没有被拆。
	if _, err := c.ResolveEndpoint(ctx, ResolveRequest{
		InterfaceID: "x", MinMajor: 1, MaxMajor: 1,
	}); err != nil {
		t.Fatalf("幽灵事件之后连接不可用: %v", err)
	}
}

// SubscriptionClosed(45) 关闭通道并带出原因。
func TestSubscription_ServerCloseCarriesReason(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(_ net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		s := env.GetSubscribe()
		if s == nil {
			return nil
		}
		return []*ipcv1.Envelope{
			{Body: &ipcv1.Envelope_SubscribeResult{SubscribeResult: &ipcv1.SubscribeResult{
				RequestId: s.GetRequestId(),
				Outcome: &ipcv1.SubscribeResult_Success{Success: &ipcv1.SubscribeSuccess{
					SubscriptionId: 11,
					DeliveryClass:  ipcv1.DeliveryClass_DELIVERY_CLASS_RELIABLE,
				}},
			}}},
			{Body: &ipcv1.Envelope_SubscriptionClosed{
				SubscriptionClosed: &ipcv1.SubscriptionClosed{
					SubscriptionId: 11,
					Reason:         ipcv1.SubscriptionClosedReason_SUBSCRIPTION_CLOSED_REASON_ENDPOINT_DIED,
				},
			}},
		}
	}))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	sub := mustSubscribe(t, c, 0)
	select {
	case _, ok := <-sub.C():
		if ok {
			t.Fatal("want closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("通道未被关闭")
	}

	var closed *SubscriptionClosedError
	if !errors.As(sub.Err(), &closed) {
		t.Fatalf("Err = %v, want *SubscriptionClosedError", sub.Err())
	}
	if closed.Reason != ipcv1.SubscriptionClosedReason_SUBSCRIPTION_CLOSED_REASON_ENDPOINT_DIED {
		t.Errorf("Reason = %v, want ENDPOINT_DIED", closed.Reason)
	}
	if !errors.Is(sub.Err(), ErrSubscriptionClosed) {
		t.Error("*SubscriptionClosedError 必须能 Unwrap 到 ErrSubscriptionClosed")
	}
}

// 连接断开时全部订阅通道被关闭，Err 指向 ErrClosed。
//
// 不关的话调用方的 range 会永久挂住，而它没有别的办法察觉连接没了。
func TestSubscription_ConnectionCloseClosesAll(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(subscribeOK(11, ipcv1.DeliveryClass_DELIVERY_CLASS_RELIABLE))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	sub := mustSubscribe(t, c, 0)
	_ = c.Close()

	select {
	case _, ok := <-sub.C():
		if ok {
			t.Fatal("want closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("连接关闭后订阅通道仍未关闭")
	}
	if !errors.Is(sub.Err(), ErrClosed) {
		t.Fatalf("Err = %v, want ErrClosed", sub.Err())
	}
}

// 退订成功后通道关闭，且 Err 为 nil——正常结束与异常终止必须可分辨。
func TestUnsubscribe_ClosesChannelWithoutError(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(func(_ net.Conn, env *ipcv1.Envelope) []*ipcv1.Envelope {
		if s := env.GetSubscribe(); s != nil {
			return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_SubscribeResult{
				SubscribeResult: &ipcv1.SubscribeResult{
					RequestId: s.GetRequestId(),
					Outcome: &ipcv1.SubscribeResult_Success{Success: &ipcv1.SubscribeSuccess{
						SubscriptionId: 11,
						DeliveryClass:  ipcv1.DeliveryClass_DELIVERY_CLASS_RELIABLE,
					}},
				},
			}}}
		}
		if u := env.GetUnsubscribe(); u != nil {
			return []*ipcv1.Envelope{{Body: &ipcv1.Envelope_UnsubscribeResult{
				UnsubscribeResult: &ipcv1.UnsubscribeResult{
					RequestId: u.GetRequestId(),
					Outcome: &ipcv1.UnsubscribeResult_Success{
						Success: &ipcv1.UnsubscribeSuccess{},
					},
				},
			}}}
		}
		return nil
	}))

	c, err := Connect(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()

	sub := mustSubscribe(t, c, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Unsubscribe(ctx, sub); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if _, ok := <-sub.C(); ok {
		t.Fatal("want closed channel")
	}
	if sub.Err() != nil {
		t.Errorf("Err = %v, want nil（主动退订是正常结束）", sub.Err())
	}
}

// ---- 提供侧 ---------------------------------------------------------------

// 未报到的 endpoint 不能推事件：endpoint_id 还是 0，nervud 无从核对归属。
func TestPublishEvent_RequiresRegisteredEndpoint(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(autoHandshake(nil))

	h, err := NewServiceHost(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("NewServiceHost: %v", err)
	}
	defer func() { _ = h.Close() }()

	e := h.NewEndpoint(RegisterRequest{InterfaceID: "x", Major: 1})
	if err := e.PublishEvent(1, nil); err == nil {
		t.Fatal("未报到的 endpoint 竟然推出了事件")
	}
}

// PublishEvent 的 wire 形状：用【Provider 自己的】endpoint 句柄，时间戳原样透传。
func TestPublishEvent_WireShape(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(registerThen(nil))

	h, err := NewServiceHost(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("NewServiceHost: %v", err)
	}
	defer func() { _ = h.Close() }()

	e := h.NewEndpoint(RegisterRequest{
		InterfaceID: "nervus.interface.motion.base", Major: 1, ResourceHandle: "base.main",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	epID, err := e.Register(ctx)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := e.PublishEventAt(5, []byte("frame"), 12345); err != nil {
		t.Fatalf("PublishEventAt: %v", err)
	}

	waitFor(t, func() bool { return f.recvCount() >= 3 })
	f.mu.Lock()
	last := f.received[len(f.received)-1]
	f.mu.Unlock()

	p := last.GetPublishEvent()
	if p == nil {
		t.Fatalf("最后一帧是 %T, want PublishEvent", last.GetBody())
	}
	if p.GetEndpointId() != epID {
		t.Errorf("endpoint_id = %d, want %d（必须是 Provider 自己的句柄）", p.GetEndpointId(), epID)
	}
	if p.GetEventId() != 5 || string(p.GetPayload()) != "frame" {
		t.Errorf("event_id = %d, payload = %q", p.GetEventId(), p.GetPayload())
	}
	if p.GetMonotonicTimestampNanos() != 12345 {
		t.Errorf("timestamp = %d, want 12345（原样透传）", p.GetMonotonicTimestampNanos())
	}
}

// 【不带时间戳时必须发 0，不能替调用方编一个】。
//
// 有意义的时刻是数据产生的那一刻；用发布时刻冒充会产出一个看起来合理、
// 实际混进了排队延迟的数字，而消费者无法分辨。
func TestPublishEvent_OmittedTimestampStaysZero(t *testing.T) {
	f := startFakeNervud(t)
	f.setHandler(registerThen(nil))

	h, err := NewServiceHost(quietConfig(f.sockPath()))
	if err != nil {
		t.Fatalf("NewServiceHost: %v", err)
	}
	defer func() { _ = h.Close() }()

	e := h.NewEndpoint(RegisterRequest{
		InterfaceID: "nervus.interface.motion.base", Major: 1, ResourceHandle: "base.main",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := e.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := e.PublishEvent(5, nil); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	waitFor(t, func() bool { return f.recvCount() >= 3 })
	f.mu.Lock()
	last := f.received[len(f.received)-1]
	f.mu.Unlock()

	if ts := last.GetPublishEvent().GetMonotonicTimestampNanos(); ts != 0 {
		t.Errorf("timestamp = %d, want 0", ts)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}
