// 本文件是握手：连接建立后的第一次往返（Hello → HelloAck）。
//
// 握手前 nervud 不接受任何其它 body，且握手有独立的短 deadline——连上不说话
// 就能白占一个连接槽。所以本文件的每一步都带超时。
package sdk

import (
	"fmt"
	"time"

	ipcv1 "github.com/nervus-os/nervus-ipc/go/protocol/ipcv1"
)

// Identity 是握手完成后 nervud 回显的、【已核对】的身份。
//
// 这不是 nervud 授予的身份——身份早在 SO_PEERCRED（内核给出的 PID/UID/GID）
// 时就定了。回显的意义是让 SDK 尽早发现配置错位（「我以为我是 com.foo.a，
// 内核说我是 com.foo.b」），否则这类错误要等到第一次权限拒绝才暴露。
type Identity struct {
	PackageID   string
	ComponentID string
}

// handshake 发 Hello 并等 HelloAck，成功后填好 conn 的协商结果。
//
// declaredComponentID 是本进程【自称】的 Component ID。它是待验证的线索，不是
// 身份声明：nervud 会用对端 PID → cgroup → systemd unit → 启动记录去核对，
// 不一致直接拒绝握手。之所以还要客户端提供，是因为同一 Package 的多个 Component
// 共享 UID，先拿到自称值可以把核对从「遍历该 Package 的全部 Component」收敛成
// 一次比对。填错不会让你冒充别人，只会让你连不上。
func (co *conn) handshake(sdkName, sdkVersion, declaredComponentID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	if err := co.c.SetDeadline(deadline); err != nil {
		return fmt.Errorf("sdk: set handshake deadline: %w", err)
	}
	// 无论成功失败都清掉握手 deadline：它只覆盖握手这一段，后续读写各自管理
	// 自己的超时。忘了清会让第一个正常请求在 deadline 到期时莫名失败。
	defer func() { _ = co.c.SetDeadline(time.Time{}) }()

	hello := &ipcv1.Envelope{
		// 握手帧【必须】带版本号——这是全协议唯一有意义的两个位置。
		ProtocolMajor: protocolMajorMax,
		ProtocolMinor: protocolMinorMax,
		Body: &ipcv1.Envelope_Hello{Hello: &ipcv1.Hello{
			MinProtocolMajor: protocolMajorMin,
			MaxProtocolMajor: protocolMajorMax,
			MaxProtocolMinor: protocolMinorMax,
			// sdk_name / sdk_version 只用于诊断与兼容性统计，【不参与任何裁决】。
			// 权限来自 Package 签名/安装来源/manifest，不来自「装了哪个 SDK」。
			SdkName:             sdkName,
			SdkVersion:          sdkVersion,
			DeclaredComponentId: declaredComponentID,
		}},
	}
	if err := co.writeEnvelope(hello); err != nil {
		return fmt.Errorf("sdk: send Hello: %w", err)
	}

	env, err := co.readEnvelope()
	if err != nil {
		return fmt.Errorf("sdk: read HelloAck: %w", err)
	}
	ack := env.GetHelloAck()
	if ack == nil {
		// 握手期间只可能收到 HelloAck。收到别的说明对端不是 nervud，或者
		// 状态机错乱——两种情况都不该继续。
		return fmt.Errorf("%w: expected HelloAck, got %T", ErrProtocol, env.GetBody())
	}

	if f := ack.GetFailure(); f != nil {
		// nervud 在版本谈不拢/身份核对失败时【先发 Failure HelloAck 再关连接】，
		// 不裸关。所以这里能拿到确切原因：UNAUTHENTICATED 通常是
		// declared_component_id 与内核事实不符，FAILED_PRECONDITION 是版本无交集。
		return fmt.Errorf("%w: %v", ErrHandshakeFailed, statusErrorFrom(f))
	}

	ok := ack.GetSuccess()
	if ok == nil {
		return fmt.Errorf("%w: HelloAck has neither success nor failure", ErrProtocol)
	}

	co.negMajor = ok.GetProtocolMajor()
	co.negMinor = ok.GetProtocolMinor()
	co.packageID = ok.GetPackageId()
	co.componentID = ok.GetComponentId()
	co.limits = ok.GetLimits()

	if co.negMajor != protocolMajorMax {
		// 服务端选了一个我们没声明支持的 major。协议规定它必须在我们给的闭区间
		// 内选，越界说明对端实现有问题——继续通信只会在某个字段上悄悄不一致。
		return fmt.Errorf("%w: server chose unsupported major %d", ErrProtocol, co.negMajor)
	}
	return nil
}

// Identity 返回握手时核对确认的身份。握手前调用返回零值。
func (co *conn) Identity() Identity {
	return Identity{PackageID: co.packageID, ComponentID: co.componentID}
}
