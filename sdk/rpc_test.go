package sdk

import (
	"errors"
	"sync"
	"testing"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

func TestRequestIDGen_StartsAtOneAndNeverUsesZero(t *testing.T) {
	// 协议规定 0 永久保留、首个请求为 1。用 0 会让接收方无法区分
	// 「没设置这个字段」与「这是第 0 号请求」——proto3 不发零值。
	g := newRequestIDGen()
	for i := uint64(1); i <= 100; i++ {
		id, err := g.nextID()
		if err != nil {
			t.Fatalf("nextID: %v", err)
		}
		if id != i {
			t.Fatalf("id = %d, want %d", id, i)
		}
	}
}

func TestRequestIDGen_ExhaustsInsteadOfWrapping(t *testing.T) {
	// 回绕会让一个迟到的响应匹配上一个新请求——两者 request_id 与连接都相同，
	// 接收方没有任何办法分辨。协议因此要求耗尽即重建连接。
	g := newRequestIDGen()
	g.next = ^uint64(0) // 直接推到最后一个可用值

	last, err := g.nextID()
	if err != nil {
		t.Fatalf("last id should still be issued: %v", err)
	}
	if last != ^uint64(0) {
		t.Fatalf("last = %d, want max uint64", last)
	}

	// 此后【永久】失败，且绝不回绕到 0 或 1
	for i := 0; i < 3; i++ {
		id, err := g.nextID()
		if !errors.Is(err, ErrRequestIDExhausted) {
			t.Fatalf("call %d: err = %v, want ErrRequestIDExhausted", i, err)
		}
		if id != 0 {
			t.Fatalf("exhausted generator must not issue an id, got %d", id)
		}
	}
}

func TestRequestIDGen_ConcurrentUniqueness(t *testing.T) {
	// 并发分配不得重号：重号意味着两个在途请求共用一个 pending 槽，
	// 后来者会覆盖前者，前者永远等不到响应。
	g := newRequestIDGen()
	const n = 500

	var wg sync.WaitGroup
	ids := make([]uint64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := g.nextID()
			if err != nil {
				t.Errorf("nextID: %v", err)
				return
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()

	seen := make(map[uint64]bool, n)
	for _, id := range ids {
		if id == 0 {
			t.Fatal("id 0 issued")
		}
		if seen[id] {
			t.Fatalf("duplicate id %d", id)
		}
		seen[id] = true
	}
}

func TestPendingMap_DeliverAndRemove(t *testing.T) {
	p := newPendingMap()
	ch := p.add(7)

	if !p.deliver(7, &ipcv1.Response{RequestId: 7}) {
		t.Fatal("deliver should hit")
	}
	if got := <-ch; got.GetRequestId() != 7 {
		t.Fatalf("delivered id = %d, want 7", got.GetRequestId())
	}

	// 协议保证每个 Request 最多一个终结 Response。第二个同 ID 响应必须落进
	// 「未命中→丢弃」，绝不能再投递一次（那会让调用方拿到两个结果）。
	if p.deliver(7, &ipcv1.Response{RequestId: 7}) {
		t.Fatal("second deliver for same id must miss")
	}
}

func TestPendingMap_LateResponseIsDroppedNotFatal(t *testing.T) {
	// 已超时走人的调用方：remove 之后收到迟到响应。deliver 必须返回 false
	// 让调用方丢弃——而不是阻塞在发送上，也不是当协议违规关连接。
	// 已超时的请求收到迟到响应是完全正常的时序。
	p := newPendingMap()
	p.add(9)
	p.remove(9)

	done := make(chan bool, 1)
	go func() { done <- p.deliver(9, &ipcv1.Response{RequestId: 9}) }()

	select {
	case hit := <-done:
		if hit {
			t.Fatal("deliver to removed id should miss")
		}
	default:
		// 走到这里说明 deliver 还没返回，下面的接收会兜住
	}
	if hit := <-done; hit {
		t.Fatal("deliver to removed id should miss")
	}
}

func TestPendingMap_OutOfOrderDelivery(t *testing.T) {
	// 乱序响应：nervud 不保证按请求顺序回。三个在途请求以 3,1,2 的顺序回来，
	// 每个都必须落到自己的 channel。
	p := newPendingMap()
	chs := map[uint64]chan *ipcv1.Response{
		1: p.add(1), 2: p.add(2), 3: p.add(3),
	}
	for _, id := range []uint64{3, 1, 2} {
		if !p.deliver(id, &ipcv1.Response{RequestId: id}) {
			t.Fatalf("deliver %d missed", id)
		}
	}
	for id, ch := range chs {
		got := <-ch
		if got.GetRequestId() != id {
			t.Errorf("channel %d got response for %d", id, got.GetRequestId())
		}
	}
}

func TestPendingMap_FailAllClosesChannels(t *testing.T) {
	// 连接断开：全部在途请求必须被唤醒。等待方用「收到 nil / channel 关闭」
	// 区分「连接没了」与「拿到了真响应」。不唤醒就是永久泄漏一个 goroutine。
	p := newPendingMap()
	a, b := p.add(1), p.add(2)
	p.failAll()

	for i, ch := range []chan *ipcv1.Response{a, b} {
		select {
		case resp, ok := <-ch:
			if ok {
				t.Errorf("channel %d: got %v, want closed", i, resp)
			}
		default:
			t.Errorf("channel %d not closed by failAll", i)
		}
	}
}
