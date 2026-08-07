package registry

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	pkgv1 "github.com/nervus-os/nervus-ipc/protocol/interface/pkgmanagerv1"
	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

func pkgManagerArtifacts(t *testing.T) (*ipcv1.ProviderDescriptor, *ipcv1.InterfaceSchemaBundleSet) {
	t.Helper()
	const interfaceID = "nervus.interface.pkg.manager"
	bundle, err := BuildSchemaBundle(interfaceID, 1, pkgv1.PackageManagerMethod(0).Descriptor())
	if err != nil {
		t.Fatalf("BuildSchemaBundle: %v", err)
	}
	descriptor := &ipcv1.ProviderDescriptor{
		PackageId: "nervus.pkgmanagerd",
		Interfaces: []*ipcv1.ProvidedInterface{{
			InterfaceId: interfaceID,
			InterfaceVersions: []*ipcv1.ProvidedInterfaceVersion{{
				Major: 1, SchemaHash: append([]byte(nil), bundle.GetSchemaHash()...),
			}},
			RequiredPermission: "perm.pkg.query",
		}},
	}
	return descriptor, &ipcv1.InterfaceSchemaBundleSet{
		Bundles: []*ipcv1.InterfaceSchemaBundle{bundle},
	}
}

// digest 覆盖的就是这两串字节，所以同一份输入必须每次编码出完全相同的结果。
// 这条不成立的话，「可重复构建」和「构建期算好 digest」两件事同时垮掉。
func TestMarshalProviderArtifactsIsDeterministic(t *testing.T) {
	descriptor, bundles := pkgManagerArtifacts(t)

	firstDesc, firstSchema, err := MarshalProviderArtifacts(descriptor, bundles)
	if err != nil {
		t.Fatalf("MarshalProviderArtifacts: %v", err)
	}
	secondDesc, secondSchema, err := MarshalProviderArtifacts(descriptor, bundles)
	if err != nil {
		t.Fatalf("MarshalProviderArtifacts again: %v", err)
	}
	if !bytes.Equal(firstDesc, secondDesc) || !bytes.Equal(firstSchema, secondSchema) {
		t.Fatal("provider artifact encoding is not deterministic")
	}
}

// 编码出来的字节必须能被 ParseProviderArtifacts 原样读回：打包器写的与内核读的
// 是同一个契约，中间没有第二种编码。
func TestMarshalProviderArtifactsRoundTrips(t *testing.T) {
	descriptor, bundles := pkgManagerArtifacts(t)

	descriptorWire, schemaWire, err := MarshalProviderArtifacts(descriptor, bundles)
	if err != nil {
		t.Fatalf("MarshalProviderArtifacts: %v", err)
	}
	artifacts, err := ParseProviderArtifacts(descriptorWire, schemaWire)
	if err != nil {
		t.Fatalf("ParseProviderArtifacts: %v", err)
	}
	if !proto.Equal(artifacts.Descriptor, descriptor) {
		t.Fatal("round-tripped descriptor differs from input")
	}
	if artifacts.Schemas.Len() != 1 {
		t.Fatalf("schema count = %d, want 1", artifacts.Schemas.Len())
	}
}

// 打包期就要挡下内核会拒的内容。这里用「接口声明的 schema_hash 与 bundle 不符」
// 作代表：等到目标机装载时才发现，镜像已经烧好了。
func TestMarshalProviderArtifactsRejectsInvalidInput(t *testing.T) {
	descriptor, bundles := pkgManagerArtifacts(t)
	descriptor.Interfaces[0].InterfaceVersions[0].SchemaHash = []byte("not-the-real-hash")

	if _, _, err := MarshalProviderArtifacts(descriptor, bundles); err == nil {
		t.Fatal("MarshalProviderArtifacts accepted a mismatched schema hash, want failure")
	}
}

func TestMarshalProviderArtifactsRejectsNil(t *testing.T) {
	descriptor, bundles := pkgManagerArtifacts(t)

	if _, _, err := MarshalProviderArtifacts(nil, bundles); err == nil {
		t.Fatal("nil descriptor accepted, want failure")
	}
	if _, _, err := MarshalProviderArtifacts(descriptor, nil); err == nil {
		t.Fatal("nil bundle set accepted, want failure")
	}
}
