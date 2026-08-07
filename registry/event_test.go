package registry

import (
	"bytes"
	"strings"
	"testing"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

func eventDescriptor(
	t *testing.T, methods []*ipcv1.MethodMeta, events []*ipcv1.EventMeta,
) *ipcv1.ProviderDescriptor {
	t.Helper()
	hash, err := ContractHash(methods, events)
	if err != nil {
		t.Fatalf("ContractHash: %v", err)
	}
	return &ipcv1.ProviderDescriptor{
		PackageId: metaPackageID,
		Interfaces: []*ipcv1.ProvidedInterface{{
			InterfaceId: metaInterfaceID,
			InterfaceVersions: []*ipcv1.ProvidedInterfaceVersion{{
				Major: 1, SchemaHash: hash, Methods: methods, Events: events,
			}},
			RequiredPermission: metaPermission,
		}},
		Permissions: []*ipcv1.DefinedPermission{{
			Id:           metaPermission,
			GrantMode:    ipcv1.GrantMode_GRANT_MODE_NORMAL,
			RiskClass:    ipcv1.RiskClass_RISK_CLASS_NORMAL,
			MinimumTrust: ipcv1.PermissionTrustFloor_PERMISSION_TRUST_FLOOR_ORDINARY,
			DisplayName:  &ipcv1.LocalizedText{ZhCn: "取流", En: "Stream"},
			Description:  &ipcv1.LocalizedText{ZhCn: "取流", En: "Stream"},
		}},
	}
}

// 带事件的元数据接口必须能完整走通 Parse，事件元数据可查。
func TestEventsParseIntoSchema(t *testing.T) {
	descriptor := eventDescriptor(t, cameraLikeMethods(), cameraLikeEvents())
	descriptorWire, schemaWire, err := MarshalProviderArtifacts(
		descriptor, &ipcv1.InterfaceSchemaBundleSet{})
	if err != nil {
		t.Fatalf("MarshalProviderArtifacts: %v", err)
	}
	artifacts, err := ParseProviderArtifacts(descriptorWire, schemaWire)
	if err != nil {
		t.Fatalf("ParseProviderArtifacts: %v", err)
	}
	schema, ok := artifacts.Schemas.Lookup(metaInterfaceID, 1)
	if !ok {
		t.Fatal("接口没有进 SchemaSet")
	}
	if len(schema.Events()) != 2 {
		t.Fatalf("事件数 = %d, want 2", len(schema.Events()))
	}
	state, ok := schema.Event(1)
	if !ok {
		t.Fatal("event 1 查不到")
	}
	if state.GetDeliveryClass() != ipcv1.DeliveryClass_DELIVERY_CLASS_STATE {
		t.Errorf("delivery_class = %v, want STATE", state.GetDeliveryClass())
	}
	if state.GetMaxEventsPerSecond() != 10 {
		t.Errorf("max_events_per_second = %d, want 10", state.GetMaxEventsPerSecond())
	}
}

// 【事件必须进契约哈希】。DeliveryClass 决定客户端看到 sequence 跳号时该
// 「什么都不做」还是「数据永久丢失」。两个 Provider 若能对同一事件声明不同类别，
// App 解析到谁就决定了它会不会漏数据。
func TestContractHashCoversEventChanges(t *testing.T) {
	base, err := ContractHash(cameraLikeMethods(), cameraLikeEvents())
	if err != nil {
		t.Fatalf("ContractHash: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func([]*ipcv1.EventMeta)
	}{
		{"改投递类别", func(e []*ipcv1.EventMeta) {
			e[0].DeliveryClass = ipcv1.DeliveryClass_DELIVERY_CLASS_LOSSY
		}},
		{"改事件权限", func(e []*ipcv1.EventMeta) {
			e[0].RequiredPermission = "com.vendor.cam.permission.other"
		}},
		{"放宽推送速率", func(e []*ipcv1.EventMeta) { e[0].MaxEventsPerSecond = 1000 }},
		{"改 event id", func(e []*ipcv1.EventMeta) { e[1].EventId = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := cameraLikeEvents()
			tc.mutate(events)
			got, err := ContractHash(cameraLikeMethods(), events)
			if err != nil {
				t.Fatalf("ContractHash: %v", err)
			}
			if bytes.Equal(got, base) {
				t.Fatalf("%s 之后契约哈希没变", tc.name)
			}
		})
	}
}

// 「3 个方法 0 个事件」与「2 个方法 1 个事件」不能撞出同一个哈希。
// 分隔符就是为这条存在的。
func TestContractHashSeparatesMethodsFromEvents(t *testing.T) {
	methodsOnly, err := ContractHash(cameraLikeMethods(), nil)
	if err != nil {
		t.Fatalf("ContractHash: %v", err)
	}
	withEvents, err := ContractHash(cameraLikeMethods(), cameraLikeEvents())
	if err != nil {
		t.Fatalf("ContractHash: %v", err)
	}
	if bytes.Equal(methodsOnly, withEvents) {
		t.Fatal("加了事件之后契约哈希没变")
	}
}

// 事件顺序不影响契约身份，理由同方法。
func TestContractHashEventOrderIndependent(t *testing.T) {
	events := cameraLikeEvents()
	forward, err := ContractHash(nil, events)
	if err != nil {
		t.Fatalf("ContractHash: %v", err)
	}
	backward, err := ContractHash(nil, []*ipcv1.EventMeta{events[1], events[0]})
	if err != nil {
		t.Fatalf("ContractHash reversed: %v", err)
	}
	if !bytes.Equal(forward, backward) {
		t.Fatal("ContractHash 受事件顺序影响")
	}
}

// 只有事件、没有方法的接口是合法的（纯观察接口）。
func TestEventOnlyInterfaceIsValid(t *testing.T) {
	if _, err := ContractHash(nil, cameraLikeEvents()); err != nil {
		t.Fatalf("纯事件接口被拒: %v", err)
	}
	if _, err := ContractHash(nil, nil); err == nil {
		t.Fatal("既无方法也无事件的接口被接受了")
	}
}

// 元数据接口的事件不得声明 payload_type：没有 schema bundle 可供解析。
func TestEventRejectsPayloadType(t *testing.T) {
	events := cameraLikeEvents()
	events[0].PayloadType = "com.vendor.cam.v1.StateChanged"
	descriptor := eventDescriptor(t, cameraLikeMethods(), events)

	_, _, err := MarshalProviderArtifacts(descriptor, &ipcv1.InterfaceSchemaBundleSet{})
	if err == nil || !strings.Contains(err.Error(), "carries no schema bundle") {
		t.Fatalf("err = %v, want 拒绝 payload 类型名", err)
	}
}

// event id 0 是保留值。
func TestEventRejectsZeroID(t *testing.T) {
	events := cameraLikeEvents()
	events[0].EventId = 0
	if _, err := NewMetadataSchema(metaInterfaceID, 1, nil, events); err == nil {
		t.Fatal("event id 0 被接受了")
	}
}
