package stdinterface_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/nervus-os/nervus-ipc/registry"
	"github.com/nervus-os/nervus-ipc/registry/stdinterface"
)

// -update 重写 committed 的 Kotlin 常量文件. 平时 (CI) 不带此 flag, 只做断言;
// 带 flag 时把 Go 侧算出的 hash 落盘, 作为 JVM 侧的唯一真源.
//
// 与 golden vectors 的 -update 同一形态, 理由也相同: 跨语言一致性由"读同一份
// 由 Go 产出的文件"保证, 不由"两边各实现一遍算法"保证.
var update = flag.Bool("update", false, "rewrite the committed Kotlin schema-hash table")

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

// TestUpdateCommittedTable 在 -update 时写盘; 否则 skip.
func TestUpdateCommittedTable(t *testing.T) {
	if !*update {
		t.Skip("run with -update to regenerate the Kotlin schema-hash table")
	}
	rendered, err := stdinterface.RenderKotlin()
	if err != nil {
		t.Fatalf("RenderKotlin: %v", err)
	}
	path := filepath.Join(repoRoot(t), stdinterface.KotlinTableFile)
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}
	if wErr := os.WriteFile(path, rendered, 0o644); wErr != nil {
		t.Fatal(wErr)
	}
	t.Logf("wrote %d interfaces to %s", len(stdinterface.All()), path)
}

// TestCommittedTableMatches 是核心门禁: committed 的 Kotlin 文件必须逐字节
// 等于本包现算的结果.
//
// 改了任何标准接口的 .proto 而没重跑生成, 这里即红. 没有这道门禁, 漂移的症状
// 是 JVM Provider 在真机上报到被拒, 而错误信息只说"schema hash 不符",
// 不会说是谁过期了.
func TestCommittedTableMatches(t *testing.T) {
	if *update {
		t.Skip("skip assertions during -update")
	}
	want, err := os.ReadFile(filepath.Join(repoRoot(t), stdinterface.KotlinTableFile))
	if err != nil {
		t.Fatalf("read committed table (run `go test ./registry/stdinterface -run TestUpdateCommittedTable -update`): %v", err)
	}
	got, err := stdinterface.RenderKotlin()
	if err != nil {
		t.Fatalf("RenderKotlin: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("committed Kotlin table is stale; rerun with -update\n--- got ---\n%s\n--- want ---\n%s",
			got, want)
	}
}

// TestHashesMatchBuildSchemaBundle 断言清单里每个接口的 hash 确实是
// registry.BuildSchemaBundleWithEvents 的输出.
//
// 这条与上面那条不同: 上面钉的是"落盘的等于现算的", 这条钉的是"现算的走的是
// 与 nervud bootstrap 和各 providergen 同一个构造函数". 两者都要 —— 只有前者
// 的话, 本包若自己另算一套 hash, 落盘与现算依然一致, 但与内核对不上.
func TestHashesMatchBuildSchemaBundle(t *testing.T) {
	entries, err := stdinterface.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != len(stdinterface.All()) {
		t.Fatalf("got %d entries, want %d", len(entries), len(stdinterface.All()))
	}
	for i, iface := range stdinterface.All() {
		bundle, berr := registry.BuildSchemaBundleWithEvents(
			iface.ID, iface.Major, iface.MethodEnum, iface.EventEnum)
		if berr != nil {
			t.Fatalf("%s: BuildSchemaBundleWithEvents: %v", iface.ID, berr)
		}
		if entries[i].ID != iface.ID || entries[i].Major != iface.Major {
			t.Errorf("entry %d = %s@%d, want %s@%d",
				i, entries[i].ID, entries[i].Major, iface.ID, iface.Major)
			continue
		}
		if string(entries[i].SchemaHash) != string(bundle.GetSchemaHash()) {
			t.Errorf("%s hash mismatch\n got=%x\nwant=%x",
				iface.ID, entries[i].SchemaHash, bundle.GetSchemaHash())
		}
		// sha256 长度是 ParseSchemaBundle 明确校验的不变量, 顺手钉住:
		// 一个长度不对的 hash 在报到时会被内核以别的理由拒绝, 更难定位
		if len(entries[i].SchemaHash) != 32 {
			t.Errorf("%s hash has %d bytes, want 32", iface.ID, len(entries[i].SchemaHash))
		}
	}
}

// TestNoDuplicateInterfaces 断言清单里没有重复的 (id, major).
//
// 重复会让 committed 表里出现两条同名常量 —— Kotlin 侧直接编译不过, 但报错
// 指向生成物而不是清单, 在这里拦住更省事.
func TestNoDuplicateInterfaces(t *testing.T) {
	seen := make(map[string]struct{})
	for _, iface := range stdinterface.All() {
		key := iface.ID
		if _, dup := seen[key]; dup {
			t.Errorf("duplicate interface %s", key)
		}
		seen[key] = struct{}{}
	}
}
