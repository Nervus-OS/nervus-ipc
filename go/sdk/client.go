// 本文件是消费侧 Client：连上 nervud，把逻辑接口 Resolve 成路由句柄，然后调用。
//
// 典型用法：
//
//	c, err := sdk.Connect(sdk.Config{ComponentID: "main"})
//	defer c.Close()
//	ep, err := c.ResolveEndpoint(ctx, sdk.ResolveRequest{
//	    InterfaceID: "nervus.interface.motion.base", MinMajor: 1, MaxMajor: 1,
//	})
//	respBytes, err := c.Call(ctx, ep.EndpointID, methodSetVelocity, reqBytes, time.Second)
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

// Config 是 Client / ServiceHost 的共用构造参数。
type Config struct {
	// SockPath 控制面 UDS。空 = DefaultSockPath。
	SockPath string
	// ComponentID 是本进程自称的 Component ID，nervud 会用内核事实核对。
	// 必填：留空会让 nervud 无法收敛核对范围，握手被拒。
	ComponentID string
	// SDKName / SDKVersion 仅用于诊断统计，不参与裁决。空则用默认值。
	SDKName    string
	SDKVersion string
	// DialTimeout / HandshakeTimeout 空则用默认值。
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration
	// Log 为 nil 时用 slog.Default()。
	Log *slog.Logger
}

const (
	defaultSDKName          = "nervus-ipc-go"
	defaultSDKVersion       = "0.1.0"
	defaultDialTimeout      = 5 * time.Second
	defaultHandshakeTimeout = 5 * time.Second
)

func (cfg *Config) applyDefaults() {
	if cfg.SDKName == "" {
		cfg.SDKName = defaultSDKName
	}
	if cfg.SDKVersion == "" {
		cfg.SDKVersion = defaultSDKVersion
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = defaultDialTimeout
	}
	if cfg.HandshakeTimeout == 0 {
		cfg.HandshakeTimeout = defaultHandshakeTimeout
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
}

// Client 是控制面的消费侧连接。方法可并发调用。
type Client struct {
	co   *conn
	ids  *requestIDGen
	pend *pendingMap
	log  *slog.Logger

	// resolves 登记在途的 ResolveEndpoint。与 Request 分开是因为两者的
	// request_id 空间虽共用生成器，但响应类型不同，压进一个 map 要做类型断言。
	resolveMu sync.Mutex
	resolves  map[uint64]chan *ipcv1.ResolveEndpointResult

	readErr  error
	readDone chan struct{}
}

// Connect 建立连接并完成握手。
func Connect(cfg Config) (*Client, error) {
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

	c := &Client{
		co:       co,
		ids:      newRequestIDGen(),
		pend:     newPendingMap(),
		resolves: make(map[uint64]chan *ipcv1.ResolveEndpointResult),
		log:      cfg.Log,
		readDone: make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Identity 返回握手时 nervud 核对确认的身份。
func (c *Client) Identity() Identity { return c.co.Identity() }

// Limits 返回本连接生效的预算（SDK 自律依据，不是服务端承诺）。
func (c *Client) Limits() *ipcv1.ConnectionLimits { return c.co.Limits() }

// Close 关闭连接。在途请求全部以 ErrClosed 失败。幂等。
func (c *Client) Close() error {
	c.co.close()
	<-c.readDone
	return nil
}

// readLoop 是本连接唯一的 reader goroutine。
func (c *Client) readLoop() {
	defer close(c.readDone)
	defer c.failAllPending()

	for {
		env, err := c.co.readEnvelope()
		if err != nil {
			select {
			case <-c.co.closed:
				// 本端主动 Close 导致的读失败，不是异常
			default:
				c.readErr = err
				c.log.Debug("sdk: read loop ended", "err", err)
			}
			c.co.close()
			return
		}

		switch body := env.GetBody().(type) {
		case *ipcv1.Envelope_Response:
			r := body.Response
			if !c.pend.deliver(r.GetRequestId(), r) {
				// 迟到/重复/未知 request_id：丢弃并记一笔，【不】关连接。
				// 已超时的请求收到迟到响应是正常时序，当违规处理会频繁踢掉连接。
				c.log.Debug("sdk: dropping unmatched Response", "request_id", r.GetRequestId())
			}

		case *ipcv1.Envelope_ResolveEndpointResult:
			c.deliverResolve(body.ResolveEndpointResult)

		case *ipcv1.Envelope_Ping:
			// 协议允许任一侧发起 Ping，服务端也会 Ping 客户端。原样回显 nonce。
			pong := &ipcv1.Envelope{Body: &ipcv1.Envelope_Pong{
				Pong: &ipcv1.Pong{Nonce: body.Ping.GetNonce()},
			}}
			if err := c.co.writeEnvelope(pong); err != nil {
				c.log.Debug("sdk: send Pong failed", "err", err)
				return
			}

		case *ipcv1.Envelope_Pong:
			// 本 SDK 目前不主动 Ping，收到 Pong 属未预期。接受并忽略而不是按违规
			// 关连接——它不承载请求也不要求回复，没有危害。

		case *ipcv1.Envelope_EndpointDied, *ipcv1.Envelope_EndpointRevoked:
			// 服务端推送的 binding 失效通知。调用方下一次 Call 会拿到 NOT_FOUND，
			// 再走 NeedsReResolve 的恢复路径，所以这里只记日志。
			// TODO: 需要主动通知时在这里加回调（不改 wire）。
			c.log.Info("sdk: endpoint invalidated by server", "body", fmt.Sprintf("%T", body))

		default:
			// 其余 body 要么是 nervud 不会发的（Subscribe 组——内核未实现），
			// 要么是只该出现在 Service 连接上的（Dispatch）。收到即协议违规。
			c.readErr = fmt.Errorf("%w: unexpected body %T on client connection", ErrProtocol, body)
			c.log.Warn("sdk: unexpected body, closing", "body", fmt.Sprintf("%T", body))
			c.co.close()
			return
		}
	}
}

func (c *Client) failAllPending() {
	c.pend.failAll()
	c.resolveMu.Lock()
	m := c.resolves
	c.resolves = make(map[uint64]chan *ipcv1.ResolveEndpointResult)
	c.resolveMu.Unlock()
	for _, ch := range m {
		close(ch)
	}
}

func (c *Client) deliverResolve(r *ipcv1.ResolveEndpointResult) {
	id := r.GetRequestId()
	c.resolveMu.Lock()
	ch, ok := c.resolves[id]
	if ok {
		delete(c.resolves, id)
	}
	c.resolveMu.Unlock()
	if !ok {
		c.log.Debug("sdk: dropping unmatched ResolveEndpointResult", "request_id", id)
		return
	}
	ch <- r
}

// ResolveRequest 是一次接口解析请求。
type ResolveRequest struct {
	// InterfaceID 逻辑接口 ID，如 "nervus.interface.motion.base"。
	InterfaceID string
	// MinMajor / MaxMajor 可接受的接口 major 版本闭区间。
	MinMajor, MaxMajor uint32
	// ResourceType / ResourceRole 选择目标 Resource。两者都留空时由 nervud
	// 隐式取 {nervus.resource.motion.base, main}——移动主线因此不必显式填。
	ResourceType, ResourceRole string
}

// Endpoint 是一次成功解析的结果。
type Endpoint struct {
	// EndpointID 是【本连接作用域】的路由句柄。断线后全部失效，重连必须重新
	// Resolve。它不靠「别人猜不到数字」获得安全性——查找键是 (连接, endpoint_id)。
	EndpointID uint64
	// Major / Minor 原子选定的接口版本。
	Major, Minor uint32
	// SchemaHash 选中接口 schema 的确定性标识。调用方应当校验它与本地生成代码
	// 一致——不一致说明两侧 schema 漂移，这类错误必须在第一次调用前暴露，
	// 而不是等到某个字段被静默丢弃。
	SchemaHash []byte
	// ResourceHandle 已解析的公开 Resource 句柄。不含 Provider PID / 设备节点 /
	// 序列号等内部信息。
	ResourceHandle string
}

// ResolveEndpoint 把逻辑接口解析成本连接上的路由句柄。
func (c *Client) ResolveEndpoint(ctx context.Context, req ResolveRequest) (Endpoint, error) {
	id, err := c.ids.nextID()
	if err != nil {
		return Endpoint{}, err
	}

	env := &ipcv1.Envelope{Body: &ipcv1.Envelope_ResolveEndpoint{
		ResolveEndpoint: &ipcv1.ResolveEndpoint{
			RequestId:         id,
			InterfaceId:       req.InterfaceID,
			MinInterfaceMajor: req.MinMajor,
			MaxInterfaceMajor: req.MaxMajor,
			Selector:          selectorOf(req.ResourceType, req.ResourceRole),
		},
	}}

	ch := make(chan *ipcv1.ResolveEndpointResult, 1)
	c.resolveMu.Lock()
	c.resolves[id] = ch
	c.resolveMu.Unlock()

	if err := c.co.writeEnvelope(env); err != nil {
		c.resolveMu.Lock()
		delete(c.resolves, id)
		c.resolveMu.Unlock()
		return Endpoint{}, err
	}

	select {
	case <-ctx.Done():
		c.resolveMu.Lock()
		delete(c.resolves, id)
		c.resolveMu.Unlock()
		return Endpoint{}, ctx.Err()

	case res, ok := <-ch:
		if !ok {
			return Endpoint{}, c.closedErr()
		}
		if f := res.GetFailure(); f != nil {
			// 失败的 error_detail 固定是 ResolveEndpointErrorDetail，调用方可用
			// StatusError.UnmarshalDetail 解出 INTERFACE_NOT_FOUND /
			// RESOURCE_NOT_FOUND / VERSION_MISMATCH 等可区分原因。
			return Endpoint{}, statusErrorFrom(f)
		}
		s := res.GetSuccess()
		if s == nil {
			return Endpoint{}, fmt.Errorf("%w: ResolveEndpointResult has neither outcome", ErrProtocol)
		}
		return Endpoint{
			EndpointID:     s.GetEndpointId(),
			Major:          s.GetInterfaceMajor(),
			Minor:          s.GetInterfaceMinor(),
			SchemaHash:     s.GetInterfaceSchemaHash(),
			ResourceHandle: s.GetResourceHandle(),
		}, nil
	}
}

// selectorOf 构造 ResourceSelector。两个字段都空时返回 nil——协议规定留空由
// nervud 取隐式默认，发一个空 selector 与不发是同一语义，但不发少几个字节。
func selectorOf(typ, role string) *ipcv1.ResourceSelector {
	if typ == "" && role == "" {
		return nil
	}
	return &ipcv1.ResourceSelector{Type: typ, Role: role}
}

// Call 发起一次方法调用，阻塞直到拿到终结响应、超时或连接断开。
//
// timeout 是【调用方期望值】：nervud 会按方法最大值收紧，两者取小。传 0 表示
// 采用方法默认值，【不表示无限】。
//
// 返回的 payload 是方法专属响应的序列化 bytes，类型由
// (endpoint_id, 接口版本, method_id) 唯一决定，调用方按已知上下文解码。
func (c *Client) Call(ctx context.Context, endpointID uint64, methodID uint32, payload []byte, timeout time.Duration) ([]byte, error) {
	id, err := c.ids.nextID()
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

	ch := c.pend.add(id)
	if err := c.co.writeEnvelope(env); err != nil {
		c.pend.remove(id)
		return nil, err
	}

	select {
	case <-ctx.Done():
		// 本端放弃等待。【注意】这不会取消对端的工作——Cancel(32) 内核未实现，
		// 所以 Service 会一直算到自己的 deadline。副作用（电机已经动了）也不会
		// 因为本端放弃而回滚。见仓库根 README 的「实现状态」表。
		c.pend.remove(id)
		return nil, ctx.Err()

	case resp, ok := <-ch:
		if !ok {
			return nil, c.closedErr()
		}
		if f := resp.GetFailure(); f != nil {
			return nil, statusErrorFrom(f)
		}
		s := resp.GetSuccess()
		if s == nil {
			return nil, fmt.Errorf("%w: Response has neither outcome", ErrProtocol)
		}
		return s.GetPayload(), nil
	}
}

// closedErr 给出连接断开的原因：有读错误就带上，否则是正常关闭。
func (c *Client) closedErr() error {
	if c.readErr != nil {
		return fmt.Errorf("%w: %v", ErrClosed, c.readErr)
	}
	return ErrClosed
}
