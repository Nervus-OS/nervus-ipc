// 本文件把 wire 上的失败（StatusCode + typed error_detail）翻成 Go error。
//
// 设计取舍：不把 StatusCode 映射成一堆 Go 哨兵错误，而是保留一个带结构的
// *StatusError。调用方要区分「该退避重试」还是「重新 Resolve」还是「彻底没戏」，
// 需要的是 code 与 typed detail 两者，压成哨兵会把 detail 丢掉。
package sdk

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// 连接与协议层面的哨兵错误。
var (
	// ErrClosed 连接已关闭（正常关闭或本端主动 Close）。
	ErrClosed = errors.New("sdk: connection closed")
	// ErrHandshakeFailed 握手被 nervud 拒绝，详情见包装的 *StatusError。
	ErrHandshakeFailed = errors.New("sdk: handshake rejected")
	// ErrProtocol 对端发来了违反协议的 Envelope（方向错误、未预期的 body 等）。
	ErrProtocol = errors.New("sdk: protocol violation by peer")
	// ErrRequestIDExhausted request_id 用尽。协议规定不回绕，必须重建连接。
	ErrRequestIDExhausted = errors.New("sdk: request id space exhausted; reconnect required")
)

// StatusError 是一次调用的失败结果。
//
// Detail 是【未解码】的 typed error_detail 字节：它的具体类型由上下文决定
// （ResolveEndpoint 失败固定是 ResolveEndpointErrorDetail，ControlLease 固定是
// ControlLeaseErrorDetail，方法调用失败则是该接口自己的 detail）。本包不猜类型，
// 由调用方按已知上下文用 UnmarshalDetail 解。
//
// 不在这里替调用方解码，是因为「按发送方自报的类型解码」正是 status.proto 明令
// 禁止的模式——类型必须由接收方根据自己的上下文确定。
type StatusError struct {
	// Code 是程序判断的唯一依据。
	Code ipcv1.StatusCode

	// PublicMessage 【仅供人阅读，不参与程序判断】（status.proto 明文规定）。
	//
	// 不要对它做 switch，也不要据它本地化分支——它由 nervud 从受审计模板生成，
	// 措辞可以随时变。要可区分的原因请用 Code + Detail。
	//
	// 【提供侧（ServiceHost）不要设置它】：协议禁止原样透传 Service/Provider 的
	// 自由文本，nervud 不会转发你写的字符串。handler 想表达细因就用 Detail。
	PublicMessage string

	// Detail 是未解码的 typed error_detail 字节，见 UnmarshalDetail。
	Detail []byte
}

func (e *StatusError) Error() string {
	if e.PublicMessage != "" {
		return fmt.Sprintf("sdk: %s: %s", e.Code, e.PublicMessage)
	}
	return fmt.Sprintf("sdk: %s", e.Code)
}

// UnmarshalDetail 把 typed error_detail 解进 m。
//
// 调用方负责选对类型：ResolveEndpoint 的失败用 *ipcv1.ResolveEndpointErrorDetail，
// 接口方法的失败用该接口的 *XxxErrorDetail。类型选错会解出垃圾而不是报错
// （protobuf 的 wire 格式不自带类型标识），所以只在【上下文确定】时调用。
//
// Detail 为空返回 false；解码失败返回 error。
func (e *StatusError) UnmarshalDetail(m proto.Message) (bool, error) {
	if len(e.Detail) == 0 {
		return false, nil
	}
	if err := proto.Unmarshal(e.Detail, m); err != nil {
		return false, fmt.Errorf("sdk: decode error_detail: %w", err)
	}
	return true, nil
}

// statusErrorFrom 把 wire 的 Failure 翻成 *StatusError。
//
// f 为 nil 时给出 INTERNAL 而不是返回 nil：一个「outcome 是 failure 但 failure
// 本身为空」的响应是对端的 bug，把它当成功处理会让调用方拿到零值当结果。
func statusErrorFrom(f *ipcv1.Failure) *StatusError {
	if f == nil {
		return &StatusError{
			Code:          ipcv1.StatusCode_STATUS_CODE_INTERNAL,
			PublicMessage: "peer sent failure outcome with no Failure body",
		}
	}
	return &StatusError{
		Code:          f.GetCode(),
		PublicMessage: f.GetPublicMessage(),
		Detail:        f.GetErrorDetail(),
	}
}

// IsRetryable 报告该错误是否值得退避后重试【同一个】调用。
//
// 刻意保守：只有 UNAVAILABLE 与 RESOURCE_EXHAUSTED 算可重试。
//
//   - DEADLINE_EXCEEDED 【不】算：调用可能已经在对端产生了副作用（电机已经动了），
//     盲目重试是危险的。是否重试由调用方按方法语义决定，不由本包代劳。
//   - FAILED_PRECONDITION 不算：前置条件没变之前重试多少次都一样。
//   - NOT_FOUND 不算：endpoint 失效应当重新 Resolve，不是重试原调用。
func IsRetryable(err error) bool {
	var se *StatusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code {
	case ipcv1.StatusCode_STATUS_CODE_UNAVAILABLE,
		ipcv1.StatusCode_STATUS_CODE_RESOURCE_EXHAUSTED:
		return true
	default:
		return false
	}
}

// NeedsReResolve 报告该错误是否意味着 endpoint binding 已失效、应当重新 Resolve。
//
// NOT_FOUND 表示 binding 不存在或目标已死；PERMISSION_DENIED 表示权限在运行期
// 被撤销——后者重新 Resolve 通常也会被拒，但让调用方走同一条恢复路径，由
// Resolve 给出确切原因，好过在这里猜。
func NeedsReResolve(err error) bool {
	var se *StatusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code {
	case ipcv1.StatusCode_STATUS_CODE_NOT_FOUND,
		ipcv1.StatusCode_STATUS_CODE_PERMISSION_DENIED:
		return true
	default:
		return false
	}
}
