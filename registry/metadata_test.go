package registry

import (
	"bytes"
	"strings"
	"testing"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
	dogv1 "github.com/nervus-os/nervus-ipc/protocol/oem/acme/dog/v1"
)

const (
	metaPackageID   = "com.vendor.cam"
	metaInterfaceID = "com.vendor.cam.interface.stream"
	metaPermission  = "com.vendor.cam.permission.stream"
)

// cameraLikeMethods 是一个典型的元数据接口：开流 + 关流，零消息类型，
// 开流方法带 Transfer 预算。这正是摄像头会长的样子。
func cameraLikeMethods() []*ipcv1.MethodMeta {
	return []*ipcv1.MethodMeta{
		{
			MethodId:           1,
			RequiredPermission: metaPermission,
			RiskClass:          ipcv1.RiskClass_RISK_CLASS_NORMAL,
			IsReadOnly:         true,
			Transfer: &ipcv1.TransferPolicy{
				Direction:         ipcv1.TransferDirection_TRANSFER_DIRECTION_PROVIDER_TO_CALLER,
				MaxStreams:        1,
				MaxPacketBytes:    4 << 20,
				MaxBytesPerSecond: 64 << 20,
			},
		},
		{
			MethodId:           2,
			RequiredPermission: metaPermission,
			RiskClass:          ipcv1.RiskClass_RISK_CLASS_NORMAL,
		},
	}
}

func metadataDescriptor(t *testing.T, methods []*ipcv1.MethodMeta) *ipcv1.ProviderDescriptor {
	t.Helper()
	hash, err := MethodsHash(methods)
	if err != nil {
		t.Fatalf("MethodsHash: %v", err)
	}
	return &ipcv1.ProviderDescriptor{
		PackageId: metaPackageID,
		Interfaces: []*ipcv1.ProvidedInterface{{
			InterfaceId: metaInterfaceID,
			InterfaceVersions: []*ipcv1.ProvidedInterfaceVersion{{
				Major: 1, SchemaHash: hash, Methods: methods,
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

// 一个完全没有 .proto 消息的接口必须能通过完整的 Parse 流程。
// 这是本次改动的全部目的：加能力不用编消息。
func TestMetadataInterfaceParsesWithoutSchemaBundle(t *testing.T) {
	descriptor := metadataDescriptor(t, cameraLikeMethods())

	// 【空的 bundle set】——没有任何 schema
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
		t.Fatal("元数据接口没有进 SchemaSet")
	}
	if schema.Files() != nil {
		t.Error("元数据接口不该带 descriptor 文件")
	}
	if len(schema.Methods()) != 2 {
		t.Fatalf("方法数 = %d, want 2", len(schema.Methods()))
	}
	open, ok := schema.Method(1)
	if !ok || open.GetTransfer().GetMaxPacketBytes() != 4<<20 {
		t.Fatalf("method 1 = %+v", open)
	}
	if open.GetRequestType() != "" || open.GetResponseType() != "" {
		t.Error("元数据接口的方法不该有消息类型名")
	}
}

// 契约身份必须与方法顺序无关：打包器与内核、两个 Provider 之间的方法顺序
// 没有理由一致，若哈希受顺序影响，症状是「两家写了一样的接口却被判成冲突」。
func TestMethodsHashIsOrderIndependent(t *testing.T) {
	methods := cameraLikeMethods()
	forward, err := MethodsHash(methods)
	if err != nil {
		t.Fatalf("MethodsHash: %v", err)
	}
	reversed := []*ipcv1.MethodMeta{methods[1], methods[0]}
	backward, err := MethodsHash(reversed)
	if err != nil {
		t.Fatalf("MethodsHash reversed: %v", err)
	}
	if !bytes.Equal(forward, backward) {
		t.Fatal("MethodsHash 受方法顺序影响")
	}
}

// 改动任何一项元数据都必须改变契约身份——这正是 sameInterfaceContract 的依据。
// 两个 Provider 声明的权限或 Transfer 预算不同时，内核必须能看出来。
func TestMethodsHashDetectsMetadataChange(t *testing.T) {
	base, err := MethodsHash(cameraLikeMethods())
	if err != nil {
		t.Fatalf("MethodsHash: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func([]*ipcv1.MethodMeta)
	}{
		{"改权限", func(m []*ipcv1.MethodMeta) { m[0].RequiredPermission = "com.vendor.cam.permission.other" }},
		{"改风险级", func(m []*ipcv1.MethodMeta) { m[0].RiskClass = ipcv1.RiskClass_RISK_CLASS_PHYSICAL_CONTROL }},
		{"放宽速率预算", func(m []*ipcv1.MethodMeta) { m[0].Transfer.MaxBytesPerSecond = 1 << 30 }},
		{"改 method id", func(m []*ipcv1.MethodMeta) { m[1].MethodId = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			methods := cameraLikeMethods()
			tc.mutate(methods)
			got, err := MethodsHash(methods)
			if err != nil {
				t.Fatalf("MethodsHash: %v", err)
			}
			if bytes.Equal(got, base) {
				t.Fatalf("%s 之后契约哈希没变", tc.name)
			}
		})
	}
}

// 声明的 schema_hash 与实际方法对不上时必须拒绝：否则一个 Provider 能用
// 别人的契约身份注册自己的方法。
func TestMetadataInterfaceRejectsHashMismatch(t *testing.T) {
	descriptor := metadataDescriptor(t, cameraLikeMethods())
	descriptor.Interfaces[0].InterfaceVersions[0].SchemaHash = bytes.Repeat([]byte{0xAB}, 32)

	if _, _, err := MarshalProviderArtifacts(
		descriptor, &ipcv1.InterfaceSchemaBundleSet{},
	); err == nil {
		t.Fatal("schema_hash 与方法不符却被接受")
	}
}

// 元数据接口不得声明消息类型名：没有 bundle 可供解析，放行只会让一个永远解不出
// 的类型名一路传到 dispatch 才失败。
func TestMetadataInterfaceRejectsMessageTypes(t *testing.T) {
	methods := cameraLikeMethods()
	methods[0].RequestType = "com.vendor.cam.v1.OpenRequest"
	descriptor := metadataDescriptor(t, methods)

	_, _, err := MarshalProviderArtifacts(descriptor, &ipcv1.InterfaceSchemaBundleSet{})
	if err == nil || !strings.Contains(err.Error(), "carries no schema bundle") {
		t.Fatalf("err = %v, want 拒绝消息类型名", err)
	}
}

// 无界的 Transfer 预算要在打包期挡下。nervud 的 Begin 会以
// "method transfer policy is unbounded" 拒绝，但那时错误出现在第一次开流。
func TestMetadataInterfaceRejectsUnboundedTransfer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ipcv1.TransferPolicy)
	}{
		{"max_streams 为 0", func(p *ipcv1.TransferPolicy) { p.MaxStreams = 0 }},
		{"max_packet_bytes 为 0", func(p *ipcv1.TransferPolicy) { p.MaxPacketBytes = 0 }},
		{"max_bytes_per_second 为 0", func(p *ipcv1.TransferPolicy) { p.MaxBytesPerSecond = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			methods := cameraLikeMethods()
			tc.mutate(methods[0].Transfer)
			descriptor := metadataDescriptor(t, methods)
			if _, _, err := MarshalProviderArtifacts(
				descriptor, &ipcv1.InterfaceSchemaBundleSet{},
			); err == nil {
				t.Fatalf("%s 却被接受", tc.name)
			}
		})
	}
}

// 同一个 (接口, major) 既内联 methods 又带 bundle：两份契约，无从判断哪份权威。
func TestMetadataInterfaceRejectsDoubleDeclaration(t *testing.T) {
	bundle, err := BuildSchemaBundle(metaInterfaceID, 1, dogv1.RawGaitMethod(0).Descriptor())
	if err != nil {
		t.Fatalf("BuildSchemaBundle: %v", err)
	}
	descriptor := metadataDescriptor(t, cameraLikeMethods())

	_, _, err = MarshalProviderArtifacts(descriptor, &ipcv1.InterfaceSchemaBundleSet{
		Bundles: []*ipcv1.InterfaceSchemaBundle{bundle},
	})
	if err == nil || !strings.Contains(err.Error(), "inline methods and a schema bundle") {
		t.Fatalf("err = %v, want 拒绝双重声明", err)
	}
}
