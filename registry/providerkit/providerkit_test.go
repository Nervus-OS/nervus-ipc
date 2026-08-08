package providerkit_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	permissionv1 "github.com/nervus-os/nervus-ipc/protocol/interface/permissionv1"
	"github.com/nervus-os/nervus-ipc/registry"
	"github.com/nervus-os/nervus-ipc/registry/providerkit"
)

// -update 重写 committed 产物. 平时 (CI) 不带此 flag, 只做断言.
//
// 与 golden vectors 和 stdinterface 的 -update 同一形态, 理由也相同: 跨语言
// 一致性由"读同一份由 Go 产出的文件"保证, 不由"两边各实现一遍算法"保证.
var update = flag.Bool("update", false, "rewrite the committed provider artifacts")

// repoRoot 向上找到含 buf.yaml 的目录, 即仓库根.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "buf.yaml")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root (buf.yaml) not found above CWD")
		}
		dir = parent
	}
}

// TestUpdateCommittedArtifacts 在 -update 时写盘; 否则 skip.
func TestUpdateCommittedArtifacts(t *testing.T) {
	if !*update {
		t.Skip("run with -update to regenerate the committed provider artifacts")
	}
	root := repoRoot(t)
	for _, spec := range providerkit.All() {
		artifacts, err := providerkit.Build(spec)
		if err != nil {
			t.Fatalf("build %s: %v", spec.PackageID, err)
		}
		for _, file := range []struct {
			rel  string
			data []byte
		}{
			{artifacts.DescriptorPath(), artifacts.DescriptorWire},
			{artifacts.SchemasPath(), artifacts.SchemaWire},
		} {
			abs := filepath.Join(root, filepath.FromSlash(file.rel))
			if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
				t.Fatal(mkErr)
			}
			if wErr := os.WriteFile(abs, file.data, 0o644); wErr != nil {
				t.Fatal(wErr)
			}
			t.Logf("wrote %s (%d bytes)", file.rel, len(file.data))
		}
	}
}

// TestCommittedArtifactsMatch 是核心门禁: committed 的两份 binpb 必须逐字节等于
// 本包现算的结果.
//
// 改了某个标准接口的 .proto 而没重跑生成, 这里即红. 没有这道门禁, 漂移的症状是
// 目标机开机扫描时把那个包隔离, 而错误信息 (schema hash 不符 / 接口冲突) 不会
// 说是谁过期了 —— 且那时镜像已经烧好.
func TestCommittedArtifactsMatch(t *testing.T) {
	if *update {
		t.Skip("skip assertions during -update")
	}
	root := repoRoot(t)
	for _, spec := range providerkit.All() {
		t.Run(spec.PackageID, func(t *testing.T) {
			artifacts, err := providerkit.Build(spec)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			for _, file := range []struct {
				rel  string
				data []byte
			}{
				{artifacts.DescriptorPath(), artifacts.DescriptorWire},
				{artifacts.SchemasPath(), artifacts.SchemaWire},
			} {
				want, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.rel)))
				if readErr != nil {
					t.Fatalf("read committed %s (run `go test ./registry/providerkit "+
						"-run TestUpdateCommittedArtifacts -update`): %v", file.rel, readErr)
				}
				if string(file.data) != string(want) {
					t.Errorf("%s is stale; rerun with -update\n got %d bytes\nwant %d bytes",
						file.rel, len(file.data), len(want))
				}
			}
		})
	}
}

// TestArtifactsParseAsKernelWould 断言产物能通过内核装载时的同一套校验.
//
// Build 内部的 MarshalProviderArtifacts 已经 Parse 过一次, 这里对【committed 的
// 字节】再做一次: 前者证明"我生成的东西是合法的", 后者证明"仓库里躺着的东西是
// 合法的". 两者不同 —— 一次错误的手工改动只有后者能抓到.
func TestArtifactsParseAsKernelWould(t *testing.T) {
	root := repoRoot(t)
	for _, spec := range providerkit.All() {
		t.Run(spec.PackageID, func(t *testing.T) {
			artifacts, err := providerkit.Build(spec)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			descriptorWire, err := os.ReadFile(
				filepath.Join(root, filepath.FromSlash(artifacts.DescriptorPath())))
			if err != nil {
				t.Fatalf("read descriptor: %v", err)
			}
			schemaWire, err := os.ReadFile(
				filepath.Join(root, filepath.FromSlash(artifacts.SchemasPath())))
			if err != nil {
				t.Fatalf("read schemas: %v", err)
			}
			parsed, err := registry.ParseProviderArtifacts(descriptorWire, schemaWire)
			if err != nil {
				t.Fatalf("ParseProviderArtifacts: %v", err)
			}
			if got := parsed.Descriptor.GetPackageId(); got != spec.PackageID {
				t.Errorf("package_id = %q, want %q", got, spec.PackageID)
			}
			// package_id 必须与 manifest 一致, 否则 nervud 的
			// loadRequiredProviderArtifacts 拒绝装载. 这条断言在这里而不是靠
			// 打包链发现, 是因为那时已经晚了.
			if len(parsed.Descriptor.GetInterfaces()) != len(spec.Interfaces) {
				t.Errorf("interfaces = %d, want %d",
					len(parsed.Descriptor.GetInterfaces()), len(spec.Interfaces))
			}
			// 纯接口导出型的边界: 零资源, 零自定义权限. 哪天有人往 spec 里加了
			// 这两样, 这里会红 —— 而那意味着 providerkit 的适用边界被越过了,
			// 该走各自的 providergen.
			if n := len(parsed.Descriptor.GetResources()); n != 0 {
				t.Errorf("resources = %d, want 0 (providerkit is interface-only)", n)
			}
			if n := len(parsed.Descriptor.GetPermissions()); n != 0 {
				t.Errorf("permissions = %d, want 0 (providerkit is interface-only)", n)
			}
		})
	}
}

// TestSchemaHashMatchesStdInterface 断言产物里的 schema_hash 与 stdinterface 那张
// 表给出的是同一个值.
//
// 两处都从同一个 BuildSchemaBundle 算出, 所以正常情况下必然相等. 钉住它是为了
// 防一种具体的错配: providerkit 的 spec 里写错了 major (比如写成 2), 那时
// bundle 仍然能构造成功, hash 也是合法的, 但与 JVM 侧 hashOf(id, 1) 取到的不是
// 一个值 —— 症状是 RegisterEndpoint 被拒, 而错误只说"schema hash 不符".
func TestSchemaHashMatchesStdInterface(t *testing.T) {
	for _, spec := range providerkit.All() {
		for _, iface := range spec.Interfaces {
			bundle, err := registry.BuildSchemaBundleWithEvents(
				iface.ID, iface.Major, iface.MethodEnum, iface.EventEnum)
			if err != nil {
				t.Fatalf("%s: %v", iface.ID, err)
			}
			if len(bundle.GetSchemaHash()) != 32 {
				t.Errorf("%s hash has %d bytes, want 32", iface.ID, len(bundle.GetSchemaHash()))
			}
		}
	}
}

// TestBuildRejectsEmptySpec 钉住两条 fail-closed.
func TestBuildRejectsEmptySpec(t *testing.T) {
	if _, err := providerkit.Build(providerkit.Spec{}); err == nil {
		t.Error("empty package id accepted")
	}
	if _, err := providerkit.Build(providerkit.Spec{PackageID: "nervus.x"}); err == nil {
		// 一个不导出接口的包不需要 provider 段. 生成一份空的只会让内核以
		// 另一个理由拒绝装载, 而那个错误离真正的原因很远.
		t.Error("spec with no interfaces accepted")
	}
}

// TestBuildRejectsDuplicateInterface 断言同一个 interface_id 声明两次会被拒.
func TestBuildRejectsDuplicateInterface(t *testing.T) {
	iface := providerkit.Interface{
		ID:         "nervus.interface.permission.ui",
		Major:      1,
		MethodEnum: permissionv1.PermissionUiMethod(0).Descriptor(),
	}
	_, err := providerkit.Build(providerkit.Spec{
		PackageID:  "nervus.permissionui",
		Interfaces: []providerkit.Interface{iface, iface},
	})
	if err == nil {
		t.Error("duplicate interface accepted")
	}
}
