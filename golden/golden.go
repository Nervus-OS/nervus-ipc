// Package golden defines the language-neutral Go↔JVM golden vectors for the
// Nervus IPC v1 wire (NRCP §22.6, §10.12 结尾).
//
// 这个文件是向量的【唯一构造真源】：它用 Go 生成代码把每个向量构造出来、
// 用【确定性】序列化写成 testdata/golden/<name>.binpb，并把每个向量的期望解码
// 字段写成 testdata/golden/vectors.json（语言无关清单）。
//
// 门禁形态（A5 §3）：
//   - Go 测试（golden_test.go）：对每个向量，构造→确定性序列化，断言等于committed
//     的 .binpb 逐字节；再 parse committed .binpb，断言字段等于期望值。
//   - JVM 测试（GoldenVectorsTest.kt）：加载【同一份】.binpb，用 protobuf-java
//     解码断言字段、再 toByteArray() 断言等于 committed 字节。
//
// 因为两侧都断言"自己构造/序列化的字节 == 同一份 committed .binpb"，所以传递地
// 保证 Go 与 JVM 逐字节一致——这正是 §22.6 的硬要求。
//
// 覆盖类别（A5 §3）：
//   - Envelope Success / Failure outcome + 通用 StatusCode 映射；
//   - typed error_detail：ResolveEndpointErrorDetail、ControlLeaseErrorDetail
//     （BaseMotion/Manipulator 的 detail 待 A2/A3 落地后按同一形态补入）；
//   - 未知 reason 兼容：error_detail 里 reason=99，外层通用 code 保持不变；
//   - 畸形 Provider detail：error_detail 为无法解析的垃圾字节，外层归一化为 INTERNAL；
//   - ControlLease wire：Acquire/Release 及其 Result（A5 §1 新增）；
//   - Transfer ticket 与数据面附着结果。
package golden

import (
	"google.golang.org/protobuf/proto"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// Kind 标识一个向量的顶层消息类型，供跨语言 loader 按名分派。
type Kind string

const (
	KindEnvelope        Kind = "envelope"
	KindSchemaBundle    Kind = "interface_schema_bundle"
	KindSchemaBundleSet Kind = "interface_schema_bundle_set"
	KindTransferHandle  Kind = "transfer_handle"
	KindAttachResult    Kind = "attach_transfer_result"
)

// Vector 是一个 golden 向量的定义。
type Vector struct {
	// Name 是稳定的向量名，也是 .binpb 文件名（去扩展名）。
	Name string
	// Kind 是顶层消息类型。
	Kind Kind
	// Build 构造该向量的 proto.Message。
	Build func() proto.Message
	// Doc 是人类可读的一句话说明，写进 vectors.json。
	Doc string
}

// marshalDetail 用确定性序列化把 typed error_detail 编成 bytes。
// 两种语言对这些无 map 的小消息产出逐字节一致的编码。
func marshalDetail(m proto.Message) []byte {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		panic(err) // 向量是固定输入，编码失败即程序 bug
	}
	return b
}

// resourceSelector 是构造 ResourceSelector 的小助手。
func resourceSelector(typ, role string) *ipcv1.ResourceSelector {
	return &ipcv1.ResourceSelector{Type: typ, Role: role}
}

// Vectors 是全部 golden 向量，顺序稳定（决定 vectors.json 的顺序）。
//
// 数值刻意用固定、可读、跨语言无歧义的取值（无随机、无时间），
// 以便 committed .binpb 长期稳定。
func Vectors() []Vector {
	return []Vector{
		// --- Success / Failure outcome + 通用 StatusCode ---
		{
			Name: "response_success_ok",
			Kind: KindEnvelope,
			Doc:  "Response 成功，code=OK，payload 非空",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
					RequestId: 1,
					Outcome: &ipcv1.Response_Success{Success: &ipcv1.Success{
						Code:    ipcv1.StatusCode_STATUS_CODE_OK,
						Payload: []byte("pong"),
					}},
				}}}
			},
		},
		{
			Name: "response_failure_permission_denied",
			Kind: KindEnvelope,
			Doc:  "Response 失败，code=PERMISSION_DENIED，带 public_message",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
					RequestId: 2,
					Outcome: &ipcv1.Response_Failure{Failure: &ipcv1.Failure{
						Code:          ipcv1.StatusCode_STATUS_CODE_PERMISSION_DENIED,
						PublicMessage: "denied",
					}},
				}}}
			},
		},
		{
			Name: "response_failure_unavailable",
			Kind: KindEnvelope,
			Doc:  "Response 失败，code=UNAVAILABLE，无 detail（断线/重启类）",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
					RequestId: 3,
					Outcome: &ipcv1.Response_Failure{Failure: &ipcv1.Failure{
						Code: ipcv1.StatusCode_STATUS_CODE_UNAVAILABLE,
					}},
				}}}
			},
		},

		// --- typed error_detail：ResolveEndpointErrorDetail ---
		{
			Name: "resolve_failure_interface_not_found",
			Kind: KindEnvelope,
			Doc:  "ResolveEndpointResult 失败，NOT_FOUND + reason=INTERFACE_NOT_FOUND",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_ResolveEndpointResult{ResolveEndpointResult: &ipcv1.ResolveEndpointResult{
					RequestId: 4,
					Outcome: &ipcv1.ResolveEndpointResult_Failure{Failure: &ipcv1.Failure{
						Code: ipcv1.StatusCode_STATUS_CODE_NOT_FOUND,
						ErrorDetail: marshalDetail(&ipcv1.ResolveEndpointErrorDetail{
							Reason: ipcv1.ResolveEndpointReason_RESOLVE_ENDPOINT_REASON_INTERFACE_NOT_FOUND,
						}),
					}},
				}}}
			},
		},
		{
			Name: "resolve_failure_version_mismatch",
			Kind: KindEnvelope,
			Doc:  "ResolveEndpointResult 失败，FAILED_PRECONDITION + reason=VERSION_MISMATCH",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_ResolveEndpointResult{ResolveEndpointResult: &ipcv1.ResolveEndpointResult{
					RequestId: 5,
					Outcome: &ipcv1.ResolveEndpointResult_Failure{Failure: &ipcv1.Failure{
						Code: ipcv1.StatusCode_STATUS_CODE_FAILED_PRECONDITION,
						ErrorDetail: marshalDetail(&ipcv1.ResolveEndpointErrorDetail{
							Reason: ipcv1.ResolveEndpointReason_RESOLVE_ENDPOINT_REASON_VERSION_MISMATCH,
						}),
					}},
				}}}
			},
		},

		// --- 未知 reason 兼容：旧 SDK 遇新 reason 保留通用 code ---
		{
			Name: "resolve_failure_unknown_reason",
			Kind: KindEnvelope,
			Doc:  "error_detail 里 reason=99（未知），外层 NOT_FOUND 不变；旧 SDK 须保留通用 code",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_ResolveEndpointResult{ResolveEndpointResult: &ipcv1.ResolveEndpointResult{
					RequestId: 6,
					Outcome: &ipcv1.ResolveEndpointResult_Failure{Failure: &ipcv1.Failure{
						Code: ipcv1.StatusCode_STATUS_CODE_NOT_FOUND,
						ErrorDetail: marshalDetail(&ipcv1.ResolveEndpointErrorDetail{
							Reason: ipcv1.ResolveEndpointReason(99),
						}),
					}},
				}}}
			},
		},

		// --- 畸形 Provider detail 归一化为 INTERNAL ---
		{
			Name: "failure_malformed_detail_internal",
			Kind: KindEnvelope,
			Doc:  "Response 失败，INTERNAL，error_detail 为无法解析的垃圾字节（nervud 归一化结果）",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_Response{Response: &ipcv1.Response{
					RequestId: 7,
					Outcome: &ipcv1.Response_Failure{Failure: &ipcv1.Failure{
						Code:        ipcv1.StatusCode_STATUS_CODE_INTERNAL,
						ErrorDetail: []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0F}, // 非法 tag/varint
					}},
				}}}
			},
		},

		// --- ControlLease wire（A5 §1）---
		{
			Name: "acquire_control_human_base",
			Kind: KindEnvelope,
			Doc:  "AcquireControl：HUMAN 申请 base.main，期望时效 500ms",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_AcquireControl{AcquireControl: &ipcv1.AcquireControl{
					RequestId:              8,
					ControllerClass:        ipcv1.ControllerClass_CONTROLLER_CLASS_HUMAN,
					Resource:               resourceSelector("nervus.resource.motion.base", "main"),
					RequestedDeadlineNanos: 500_000_000,
				}}}
			},
		},
		{
			Name: "acquire_control_ai_arm",
			Kind: KindEnvelope,
			Doc:  "AcquireControl：AI 申请 arm.main，期望时效 1s",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_AcquireControl{AcquireControl: &ipcv1.AcquireControl{
					RequestId:              9,
					ControllerClass:        ipcv1.ControllerClass_CONTROLLER_CLASS_AI,
					Resource:               resourceSelector("nervus.resource.manipulator", "main"),
					RequestedDeadlineNanos: 1_000_000_000,
				}}}
			},
		},
		{
			Name: "acquire_control_result_success",
			Kind: KindEnvelope,
			Doc:  "AcquireControlResult 成功：lease_id/motion_epoch/deadline/resource_handle",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_AcquireControlResult{AcquireControlResult: &ipcv1.AcquireControlResult{
					RequestId: 10,
					Outcome: &ipcv1.AcquireControlResult_Success{Success: &ipcv1.AcquireControlSuccess{
						LeaseId:        0xABCD,
						MotionEpoch:    3,
						DeadlineNanos:  1_234_567_890,
						ResourceHandle: "base.main",
					}},
				}}}
			},
		},
		{
			Name: "acquire_control_result_held_by_human",
			Kind: KindEnvelope,
			Doc:  "AcquireControlResult 失败：FAILED_PRECONDITION + reason=HELD_BY_HUMAN（可区分原因）",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_AcquireControlResult{AcquireControlResult: &ipcv1.AcquireControlResult{
					RequestId: 11,
					Outcome: &ipcv1.AcquireControlResult_Failure{Failure: &ipcv1.Failure{
						Code: ipcv1.StatusCode_STATUS_CODE_FAILED_PRECONDITION,
						ErrorDetail: marshalDetail(&ipcv1.ControlLeaseErrorDetail{
							Reason: ipcv1.ControlLeaseErrorReason_CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN,
						}),
					}},
				}}}
			},
		},
		{
			Name: "acquire_control_result_unknown_reason",
			Kind: KindEnvelope,
			Doc:  "AcquireControlResult 失败：control-lease reason=99（未知），外层 FAILED_PRECONDITION 不变",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_AcquireControlResult{AcquireControlResult: &ipcv1.AcquireControlResult{
					RequestId: 12,
					Outcome: &ipcv1.AcquireControlResult_Failure{Failure: &ipcv1.Failure{
						Code: ipcv1.StatusCode_STATUS_CODE_FAILED_PRECONDITION,
						ErrorDetail: marshalDetail(&ipcv1.ControlLeaseErrorDetail{
							Reason: ipcv1.ControlLeaseErrorReason(99),
						}),
					}},
				}}}
			},
		},
		{
			Name: "release_control",
			Kind: KindEnvelope,
			Doc:  "ReleaseControl：释放本连接持有的 lease",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_ReleaseControl{ReleaseControl: &ipcv1.ReleaseControl{
					RequestId: 13,
					LeaseId:   0xABCD,
				}}}
			},
		},
		{
			Name: "release_control_result_success",
			Kind: KindEnvelope,
			Doc:  "ReleaseControlResult 成功，code=OK",
			Build: func() proto.Message {
				return &ipcv1.Envelope{Body: &ipcv1.Envelope_ReleaseControlResult{ReleaseControlResult: &ipcv1.ReleaseControlResult{
					RequestId: 14,
					Outcome: &ipcv1.ReleaseControlResult_Success{Success: &ipcv1.Success{
						Code: ipcv1.StatusCode_STATUS_CODE_OK,
					}},
				}}}
			},
		},
		{
			Name: "dispatch_control_execution_context",
			Kind: KindEnvelope,
			Doc:  "Dispatch 携带内核签发的 ControlLease 执行上下文",
			Build: func() proto.Message {
				return &ipcv1.Envelope{
					ProtocolMajor: 1,
					ProtocolMinor: 1,
					Body: &ipcv1.Envelope_Dispatch{Dispatch: &ipcv1.Dispatch{
						RouteId:     15,
						EndpointId:  23,
						MethodId:    7,
						RemainingMs: 250,
						Payload:     []byte("move"),
						ExecutionContext: &ipcv1.ExecutionContext{
							LeaseId:            0xABCD,
							ControllerClass:    ipcv1.ControllerClass_CONTROLLER_CLASS_HUMAN,
							MotionEpoch:        3,
							DeadlineNanos:      1_234_567_890,
							CommandSequence:    17,
							ResourceHandle:     "base.main",
							ResourceGeneration: 4,
						},
					}},
				}
			},
		},

		// --- schema bundle 分发（A5 §2）---
		{
			Name: "interface_schema_bundle",
			Kind: KindSchemaBundle,
			Doc:  "单个 OEM 私有接口的 InterfaceSchemaBundle（interface_id/version/schema_hash/fds）",
			Build: func() proto.Message {
				return &ipcv1.InterfaceSchemaBundle{
					InterfaceId:       "com.acme.interface.gripper",
					Version:           1,
					SchemaHash:        []byte{0xDE, 0xAD, 0xBE, 0xEF},
					FileDescriptorSet: []byte{0x0A, 0x03, 'a', 'b', 'c'}, // 占位 fds 字节
					MethodEnumType:    "com.acme.gripper.v1.GripperMethod",
				}
			},
		},
		{
			Name: "interface_schema_bundle_set",
			Kind: KindSchemaBundleSet,
			Doc:  "一个 .nspkg 携带的多接口 bundle 集合",
			Build: func() proto.Message {
				return &ipcv1.InterfaceSchemaBundleSet{Bundles: []*ipcv1.InterfaceSchemaBundle{
					{
						InterfaceId:       "nervus.interface.motion.base",
						Version:           1,
						SchemaHash:        []byte{0x01, 0x02, 0x03, 0x04},
						FileDescriptorSet: []byte{0x0A, 0x01, 'x'},
						MethodEnumType:    "nervus.interface.motion.v1.BaseMotionMethod",
					},
					{
						InterfaceId:       "com.acme.interface.gripper",
						Version:           2,
						SchemaHash:        []byte{0xDE, 0xAD, 0xBE, 0xEF},
						FileDescriptorSet: []byte{0x0A, 0x01, 'y'},
						MethodEnumType:    "com.acme.gripper.v2.GripperMethod",
					},
				}}
			},
		},

		// --- 通用 Transfer 数据面 ---
		{
			Name: "transfer_handle_peer",
			Kind: KindTransferHandle,
			Doc:  "双向 Transfer 的一次性 PEER handle",
			Build: func() proto.Message {
				return &ipcv1.TransferHandle{
					TransferId:              []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
					AttachTicket:            []byte("0123456789abcdef0123456789abcdef"),
					Role:                    ipcv1.TransferRole_TRANSFER_ROLE_PEER,
					Mode:                    ipcv1.TransferMode_TRANSFER_MODE_FRAMED_RELAY,
					ExpiresAtMonotonicNanos: 9_876_543_210,
					DataPlaneEndpoint:       "/run/nervus/nervud-transfer.sock",
				}
			},
		},
		{
			Name: "attach_transfer_result_success",
			Kind: KindAttachResult,
			Doc:  "Transfer 附着成功并返回收紧后的包大小与速率",
			Build: func() proto.Message {
				return &ipcv1.AttachTransferResult{
					Outcome: &ipcv1.AttachTransferResult_Success{
						Success: &ipcv1.AttachTransferSuccess{
							Mode:              ipcv1.TransferMode_TRANSFER_MODE_FRAMED_RELAY,
							MaxPacketBytes:    1_048_576,
							MaxBytesPerSecond: 8_388_608,
						},
					},
				}
			},
		},
	}
}
