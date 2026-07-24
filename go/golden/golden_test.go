package golden_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/nervus-os/nervus-ipc/go/golden"
	ipcv1 "github.com/nervus-os/nervus-ipc/go/protocol/ipcv1"
)

// -update 重写 committed 的 .binpb 与 vectors.json。平时（CI）不带此 flag，
// 只做断言；带 flag 时把 Go 侧构造的确定性字节落盘，作为跨语言真源。
var update = flag.Bool("update", false, "rewrite golden .binpb and vectors.json")

// goldenDir 返回 repo 根下的 testdata/golden。测试 CWD 是本包目录
// (go/golden)，向上找到含 buf.yaml 的目录即 repo 根。
func goldenDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "buf.yaml")); err == nil {
			return filepath.Join(dir, "testdata", "golden")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root (buf.yaml) not found above CWD")
		}
		dir = parent
	}
}

func marshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// manifestEntry 是 vectors.json 的一条：语言无关的向量清单，供 JVM 侧按名加载。
type manifestEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Doc  string `json:"doc"`
}

// TestGoldenUpdate 在 -update 时写盘；否则 skip。
func TestGoldenUpdate(t *testing.T) {
	if !*update {
		t.Skip("run with -update to regenerate golden files")
	}
	dir := goldenDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var manifest []manifestEntry
	for _, v := range golden.Vectors() {
		file := v.Name + ".binpb"
		b := marshal(t, v.Build())
		if err := os.WriteFile(filepath.Join(dir, file), b, 0o644); err != nil {
			t.Fatal(err)
		}
		manifest = append(manifest, manifestEntry{
			Name: v.Name, Kind: string(v.Kind), File: file, Doc: v.Doc,
		})
	}
	mj, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mj = append(mj, '\n')
	if err := os.WriteFile(filepath.Join(dir, "vectors.json"), mj, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d golden vectors + manifest to %s", len(manifest), dir)
}

// TestGoldenBytesMatch 是核心门禁：每个向量 Go 构造→确定性序列化，
// 必须逐字节等于 committed .binpb。JVM 侧对同一份文件做同样断言，
// 于是 Go 与 JVM 传递地逐字节一致。
func TestGoldenBytesMatch(t *testing.T) {
	if *update {
		t.Skip("skip assertions during -update")
	}
	dir := goldenDir(t)
	for _, v := range golden.Vectors() {
		t.Run(v.Name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join(dir, v.Name+".binpb"))
			if err != nil {
				t.Fatalf("read golden (run `go test -run TestGoldenUpdate -update`): %v", err)
			}
			got := marshal(t, v.Build())
			if string(got) != string(want) {
				t.Errorf("bytes mismatch\n got=%x\nwant=%x", got, want)
			}
		})
	}
}

// TestGoldenDecode 断言 committed 字节能被解回、且关键字段符合预期，
// 覆盖 A5 §3 的解码方向（未知 reason 保留通用 code、畸形 detail 归一化等）。
func TestGoldenDecode(t *testing.T) {
	dir := goldenDir(t)
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name+".binpb"))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return b
	}

	// response_success_ok
	{
		var env ipcv1.Envelope
		if err := proto.Unmarshal(read("response_success_ok"), &env); err != nil {
			t.Fatal(err)
		}
		s := env.GetResponse().GetSuccess()
		if s.GetCode() != ipcv1.StatusCode_STATUS_CODE_OK || string(s.GetPayload()) != "pong" {
			t.Errorf("response_success_ok decoded wrong: %+v", s)
		}
	}

	// unknown reason: 外层 code 保持通用 NOT_FOUND，内层 reason=99 原样保留
	{
		var env ipcv1.Envelope
		if err := proto.Unmarshal(read("resolve_failure_unknown_reason"), &env); err != nil {
			t.Fatal(err)
		}
		f := env.GetResolveEndpointResult().GetFailure()
		if f.GetCode() != ipcv1.StatusCode_STATUS_CODE_NOT_FOUND {
			t.Errorf("unknown reason must keep generic code NOT_FOUND, got %v", f.GetCode())
		}
		var d ipcv1.ResolveEndpointErrorDetail
		if err := proto.Unmarshal(f.GetErrorDetail(), &d); err != nil {
			t.Fatalf("detail must still decode: %v", err)
		}
		if int32(d.GetReason()) != 99 {
			t.Errorf("unknown reason must round-trip as 99, got %d", d.GetReason())
		}
	}

	// 畸形 detail：外层 INTERNAL，detail 无法解析为 ResolveEndpointErrorDetail
	{
		var env ipcv1.Envelope
		if err := proto.Unmarshal(read("failure_malformed_detail_internal"), &env); err != nil {
			t.Fatal(err)
		}
		f := env.GetResponse().GetFailure()
		if f.GetCode() != ipcv1.StatusCode_STATUS_CODE_INTERNAL {
			t.Errorf("malformed detail must be normalized to INTERNAL, got %v", f.GetCode())
		}
	}

	// AcquireControl HUMAN base
	{
		var env ipcv1.Envelope
		if err := proto.Unmarshal(read("acquire_control_human_base"), &env); err != nil {
			t.Fatal(err)
		}
		a := env.GetAcquireControl()
		if a.GetControllerClass() != ipcv1.ControllerClass_CONTROLLER_CLASS_HUMAN {
			t.Errorf("controller_class wrong: %v", a.GetControllerClass())
		}
		if a.GetResource().GetType() != "nervus.resource.motion.base" || a.GetResource().GetRole() != "main" {
			t.Errorf("resource wrong: %+v", a.GetResource())
		}
		if a.GetRequestedDeadlineNanos() != 500_000_000 {
			t.Errorf("deadline wrong: %d", a.GetRequestedDeadlineNanos())
		}
	}

	// AcquireControlResult held_by_human：可区分原因
	{
		var env ipcv1.Envelope
		if err := proto.Unmarshal(read("acquire_control_result_held_by_human"), &env); err != nil {
			t.Fatal(err)
		}
		f := env.GetAcquireControlResult().GetFailure()
		if f.GetCode() != ipcv1.StatusCode_STATUS_CODE_FAILED_PRECONDITION {
			t.Errorf("code wrong: %v", f.GetCode())
		}
		var d ipcv1.ControlLeaseErrorDetail
		if err := proto.Unmarshal(f.GetErrorDetail(), &d); err != nil {
			t.Fatal(err)
		}
		if d.GetReason() != ipcv1.ControlLeaseErrorReason_CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN {
			t.Errorf("reason must be HELD_BY_HUMAN, got %v", d.GetReason())
		}
	}

	// InterfaceSchemaBundle
	{
		var b ipcv1.InterfaceSchemaBundle
		if err := proto.Unmarshal(read("interface_schema_bundle"), &b); err != nil {
			t.Fatal(err)
		}
		if b.GetInterfaceId() != "com.acme.interface.gripper" || b.GetVersion() != 1 {
			t.Errorf("bundle id/version wrong: %+v", &b)
		}
	}
}
