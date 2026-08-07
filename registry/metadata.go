package registry

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"

	"google.golang.org/protobuf/proto"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// 元数据接口：只声明方法的 id、权限、风险与 Transfer 预算，不带任何 protobuf
// 消息类型，也不需要 schema bundle。
//
// 内核真正需要知道的只有三件事：谁在调（身份）、允不允许（权限）、能开多大的
// 管子（Transfer 预算）。这三件都在 MethodMeta 里，与消息形状无关。摄像头帧、
// 麦克风采样、雷达点云本来就走 Transfer 数据面的不透明字节——为它们编一套永远
// 用不上的 Request/Response 消息，只是让「加一个能力」平白多一份 .proto 要维护。
//
// 契约身份仍然存在，只是改由方法元数据算出（MethodsHash）。因此 nervud 的
// sameInterfaceContract 照旧成立，「厂商可互换」这个性质完全保留。

// MethodsHash 计算一组 MethodMeta 的契约哈希。
//
// # 为什么必须自己排序而不是信调用方
//
// 这个哈希是【跨进程、跨仓库、跨构建】比对的契约身份：打包器算一次写进
// descriptor，内核装载时再算一次核对，两个 Provider 之间还要逐字节相同。
// 只要有一方的方法顺序不同就会得出不同的哈希，而症状是「两家明明写了一样的
// 接口却被内核判成冲突」——极难查。因此这里按 method_id 排序后再喂哈希，
// 顺序对结果没有影响。
//
// 每条 MethodMeta 用 Deterministic 编码；长度前缀防止相邻两条的字节拼接产生歧义
// （否则 {id:1}{id:23} 与 {id:12}{id:3} 有可能撞出同一串输入）。
func MethodsHash(methods []*ipcv1.MethodMeta) ([]byte, error) {
	return ContractHash(methods, nil)
}

// ContractHash 计算一个接口 major 的完整契约哈希：方法【与事件】一起进。
//
// 事件必须进哈希：DeliveryClass 决定客户端看到 sequence 跳号时该「什么都不做」
// 还是「数据永久丢失」。两个 Provider 若能对同一个事件声明不同的类别，
// App 解析到谁就决定了它会不会漏数据——而那正是 sameInterfaceContract 要挡的。
//
// 方法与事件分段喂入，各自按 id 排序：两者是独立的编号空间，混在一起排序会让
// 「method 1」和「event 1」的相对位置取决于实现细节。
func ContractHash(methods []*ipcv1.MethodMeta, events []*ipcv1.EventMeta) ([]byte, error) {
	if len(methods) == 0 && len(events) == 0 {
		return nil, errors.New("registry: metadata interface has neither methods nor events")
	}
	marshal := proto.MarshalOptions{Deterministic: true}
	sum := sha256.New()

	writeChunk := func(wire []byte) {
		// 长度前缀防止相邻两条的字节拼接产生歧义
		var lengthPrefix [8]byte
		for i := 0; i < 8; i++ {
			lengthPrefix[i] = byte(len(wire) >> (8 * (7 - i)))
		}
		sum.Write(lengthPrefix[:])
		sum.Write(wire)
	}

	orderedMethods := make([]*ipcv1.MethodMeta, len(methods))
	copy(orderedMethods, methods)
	sort.Slice(orderedMethods, func(i, j int) bool {
		return orderedMethods[i].GetMethodId() < orderedMethods[j].GetMethodId()
	})
	for _, meta := range orderedMethods {
		wire, err := marshal.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("registry: marshal method %d: %w", meta.GetMethodId(), err)
		}
		writeChunk(wire)
	}

	// 分隔符：没有它，「3 个方法 0 个事件」与「2 个方法 1 个事件」在某些
	// 编码下可能喂出同一串输入
	sum.Write([]byte("\x00events\x00"))

	orderedEvents := make([]*ipcv1.EventMeta, len(events))
	copy(orderedEvents, events)
	sort.Slice(orderedEvents, func(i, j int) bool {
		return orderedEvents[i].GetEventId() < orderedEvents[j].GetEventId()
	})
	for _, meta := range orderedEvents {
		wire, err := marshal.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("registry: marshal event %d: %w", meta.GetEventId(), err)
		}
		writeChunk(wire)
	}
	return sum.Sum(nil), nil
}

// NewMetadataSchema 从内联方法元数据构造一个 Schema。
//
// 返回的 Schema 的 files 为 nil：没有任何消息类型可供解析。消费者
// （nervud catalog 的 findMessage）对空的 request/response 类型名直接返回 nil，
// 因此不会走到 Files() —— 而元数据接口本就禁止声明这些类型名。
func NewMetadataSchema(
	interfaceID string, major uint32,
	metas []*ipcv1.MethodMeta, eventMetas []*ipcv1.EventMeta,
) (*Schema, error) {
	if interfaceID == "" {
		return nil, errors.New("registry: empty interface id")
	}
	if major == 0 {
		return nil, errors.New("registry: interface major 0 is reserved")
	}
	hash, err := ContractHash(metas, eventMetas)
	if err != nil {
		return nil, err
	}

	methods := make(map[uint32]*ipcv1.MethodMeta, len(metas))
	for _, meta := range metas {
		if err := validateMetadataMethod(meta); err != nil {
			return nil, fmt.Errorf("registry: interface %q@%d: %w", interfaceID, major, err)
		}
		if _, dup := methods[meta.GetMethodId()]; dup {
			return nil, fmt.Errorf("registry: interface %q@%d duplicate method id %d",
				interfaceID, major, meta.GetMethodId())
		}
		methods[meta.GetMethodId()] = proto.Clone(meta).(*ipcv1.MethodMeta)
	}

	events := make(map[uint32]*ipcv1.EventMeta, len(eventMetas))
	for _, meta := range eventMetas {
		if err := validateMetadataEvent(meta); err != nil {
			return nil, fmt.Errorf("registry: interface %q@%d: %w", interfaceID, major, err)
		}
		if _, dup := events[meta.GetEventId()]; dup {
			return nil, fmt.Errorf("registry: interface %q@%d duplicate event id %d",
				interfaceID, major, meta.GetEventId())
		}
		events[meta.GetEventId()] = proto.Clone(meta).(*ipcv1.EventMeta)
	}

	return &Schema{
		bundle: &ipcv1.InterfaceSchemaBundle{
			InterfaceId: interfaceID,
			Version:     major,
			SchemaHash:  hash,
		},
		files:   nil,
		methods: methods,
		events:  events,
	}, nil
}

// validateMetadataEvent 与 validateMetadataMethod 同规。
func validateMetadataEvent(meta *ipcv1.EventMeta) error {
	if meta.GetEventId() == 0 {
		return errors.New("event id 0 is reserved")
	}
	if !knownRiskClass(meta.GetRiskClass()) {
		return fmt.Errorf("event %d has invalid risk class %d",
			meta.GetEventId(), meta.GetRiskClass())
	}
	if name := meta.GetPayloadType(); name != "" {
		return fmt.Errorf(
			"event %d declares payload type %q, but a metadata interface carries no schema bundle",
			meta.GetEventId(), name)
	}
	return nil
}

// addMetadataSchemas 把 descriptor 里所有内联了 methods 的接口版本合成为 Schema，
// 放进既有的 SchemaSet。
//
// 合成而不是新开一条通道：nervud 的 Catalog Builder 只认 SchemaSet，两种接口
// 走同一个出口，内核那侧因此一行都不用改。
func addMetadataSchemas(descriptor *ipcv1.ProviderDescriptor, schemas *SchemaSet) error {
	if descriptor == nil || schemas == nil {
		return errors.New("registry: nil descriptor or schema set")
	}
	for _, iface := range descriptor.GetInterfaces() {
		for _, version := range iface.GetInterfaceVersions() {
			if len(version.GetMethods()) == 0 && len(version.GetEvents()) == 0 {
				continue
			}
			schema, err := NewMetadataSchema(
				iface.GetInterfaceId(), version.GetMajor(),
				version.GetMethods(), version.GetEvents())
			if err != nil {
				return err
			}
			key := SchemaKey{InterfaceID: iface.GetInterfaceId(), Major: version.GetMajor()}
			if _, dup := schemas.byKey[key]; dup {
				// 同一个 (接口, major) 既内联了 methods 又带了 bundle：两份契约，
				// 无从判断哪份权威。拒绝比挑一份更安全
				return fmt.Errorf(
					"registry: interface %q@%d declares inline methods and a schema bundle",
					key.InterfaceID, key.Major)
			}
			if schemas.byKey == nil {
				schemas.byKey = make(map[SchemaKey]*Schema, 1)
			}
			schemas.byKey[key] = schema
		}
	}
	return nil
}

// validateMetadataMethod 是 validateMethodMeta 的元数据接口版本：同样的通用检查，
// 但额外禁止声明消息类型名——没有 schema bundle 可供解析它们，声明了也无从校验，
// 放行只会让一个永远解不出来的类型名一路传到 dispatch 才失败。
func validateMetadataMethod(meta *ipcv1.MethodMeta) error {
	if meta.GetMethodId() == 0 {
		return errors.New("method id 0 is reserved")
	}
	if !knownRiskClass(meta.GetRiskClass()) {
		return fmt.Errorf("method %d has invalid risk class %d", meta.GetMethodId(), meta.GetRiskClass())
	}
	if timeMax := meta.GetMaxTimeoutMs(); timeMax != 0 &&
		meta.GetDefaultTimeoutMs() != 0 && meta.GetDefaultTimeoutMs() > timeMax {
		return fmt.Errorf("method %d default timeout exceeds maximum", meta.GetMethodId())
	}
	for label, name := range map[string]string{
		"request":      meta.GetRequestType(),
		"response":     meta.GetResponseType(),
		"error detail": meta.GetErrorDetailType(),
	} {
		if name != "" {
			return fmt.Errorf(
				"method %d declares %s type %q, but a metadata interface carries no schema bundle",
				meta.GetMethodId(), label, name)
		}
	}
	if transfer := meta.GetTransfer(); transfer != nil {
		if err := validateTransferPolicy(meta.GetMethodId(), transfer); err != nil {
			return err
		}
	}
	return nil
}

// validateTransferPolicy 在打包期就挡下无界的 Transfer 声明。
//
// nervud 的 transfer.Manager.Begin 对 max_streams / max_packet_bytes /
// max_bytes_per_second 任一为 0 都会以 "method transfer policy is unbounded" 拒绝。
// 那时错误出现在第一次开流，离「manifest 里少写了一项」很远。
func validateTransferPolicy(methodID uint32, policy *ipcv1.TransferPolicy) error {
	if policy.GetMaxStreams() == 0 {
		return fmt.Errorf("method %d transfer policy has max_streams = 0", methodID)
	}
	if policy.GetMaxPacketBytes() == 0 {
		return fmt.Errorf("method %d transfer policy has max_packet_bytes = 0", methodID)
	}
	if policy.GetMaxBytesPerSecond() == 0 {
		return fmt.Errorf("method %d transfer policy has max_bytes_per_second = 0", methodID)
	}
	return nil
}
