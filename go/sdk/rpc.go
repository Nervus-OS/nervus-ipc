// 本文件是请求 ID 分配与在途请求登记（pending map）。
//
// 匹配键在协议上是 (连接, request_id)，所以本文件的两个结构都【属于一条连接】，
// 不做进程级全局。不同连接上的相同数字互不冲突，即使两个进程同属一个 Package、
// 共享 UID。
package sdk

import (
	"sync"

	ipcv1 "github.com/nervus-os/nervus-ipc/go/protocol/ipcv1"
)

// requestIDGen 是连接内的原子递增 request_id 分配器。
//
// 协议规定（envelope.proto Request.request_id）：
//   - 【0 永久保留】，首个请求为 1
//   - 【绝不回绕】：达到上限时关闭并重建连接
//
// 不回绕这条不是洁癖。回绕会让一个迟到的响应匹配上一个新请求——两者的
// request_id 相同、连接也相同，接收方没有任何办法分辨。宁可重建连接。
type requestIDGen struct {
	mu   sync.Mutex
	next uint64
}

func newRequestIDGen() *requestIDGen { return &requestIDGen{next: 1} }

// nextID 返回下一个 request_id。用尽返回 ErrRequestIDExhausted。
//
// 用 mutex 而不是 atomic.AddUint64：溢出检测需要「读-判断-写」原子完成，
// 而 atomic.Add 溢出后会静默回绕到 0——正好是协议禁止的两件事（回绕 + 用 0）。
func (g *requestIDGen) nextID() (uint64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.next == 0 {
		// 上一次分配把计数推到了尽头（见下方回绕保护），此后永久拒绝
		return 0, ErrRequestIDExhausted
	}
	id := g.next
	if id == ^uint64(0) {
		g.next = 0 // 标记耗尽，下一次调用直接失败
	} else {
		g.next = id + 1
	}
	return id, nil
}

// pendingMap 登记在途请求：request_id → 等待响应的 channel。
//
// 容量 1 的缓冲 channel：reader goroutine 投递响应时【绝不能阻塞】。若用无缓冲
// channel，一个已经超时走人的调用方会让 reader 永久卡在发送上，整条连接上所有
// 其它请求跟着饿死。缓冲 1 + 一次性投递保证 reader 永不阻塞。
type pendingMap struct {
	mu sync.Mutex
	m  map[uint64]chan *ipcv1.Response
}

func newPendingMap() *pendingMap {
	return &pendingMap{m: make(map[uint64]chan *ipcv1.Response)}
}

// add 登记一个在途请求，返回接收响应的 channel。
func (p *pendingMap) add(id uint64) chan *ipcv1.Response {
	ch := make(chan *ipcv1.Response, 1)
	p.mu.Lock()
	p.m[id] = ch
	p.mu.Unlock()
	return ch
}

// remove 注销一个在途请求。调用方超时/取消时必须调用，否则 map 会泄漏。
func (p *pendingMap) remove(id uint64) {
	p.mu.Lock()
	delete(p.m, id)
	p.mu.Unlock()
}

// deliver 把响应投递给等待者，返回是否命中。
//
// 未命中（迟到的、重复的、或对方压根没发过的 request_id）返回 false 由调用方
// 记日志后【丢弃】——绝不能因此关闭连接：一个已超时的请求收到迟到响应是完全
// 正常的时序，把它当协议违规会让正常的慢响应频繁踢掉连接。
//
// 命中后立即从 map 删除：协议保证每个 Request 最多只有一个终结 Response，
// 删除让第二个同 ID 响应自然落进「未命中→丢弃」。
func (p *pendingMap) deliver(id uint64, resp *ipcv1.Response) bool {
	p.mu.Lock()
	ch, ok := p.m[id]
	if ok {
		delete(p.m, id)
	}
	p.mu.Unlock()
	if !ok {
		return false
	}
	ch <- resp // 容量 1 且每个 id 只投一次，不会阻塞
	return true
}

// failAll 让全部在途请求以同一个原因失败，用于连接断开。
//
// 关闭 channel 而不是投递错误值：等待方用「收到 nil」区分「连接没了」与
// 「收到了真正的响应」，比再定义一个哨兵响应干净。
func (p *pendingMap) failAll() {
	p.mu.Lock()
	m := p.m
	p.m = make(map[uint64]chan *ipcv1.Response)
	p.mu.Unlock()
	for _, ch := range m {
		close(ch)
	}
}
