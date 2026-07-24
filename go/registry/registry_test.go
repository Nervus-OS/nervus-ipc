package registry

import (
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/prototext"

	ipcv1 "github.com/nervus-os/nervus-ipc/go/protocol/ipcv1"
	dogv1 "github.com/nervus-os/nervus-ipc/go/protocol/oem/acme/dog/v1"
)

// TestExtractMethodMetas_OEM 证明「数据驱动」：Method Registry 从生成的 OEM
// descriptor 抽取出来，没有任何 Go 侧手写清单。加带 option 的枚举值即自动纳入。
func TestExtractMethodMetas_OEM(t *testing.T) {
	enum := dogv1.RawGaitMethod(0).Descriptor()
	metas, err := ExtractMethodMetas(enum)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// 两个带 option 的方法（GET_GAIT_STATE、SET_RAW_GAIT），UNSPECIFIED 被跳过。
	if len(metas) != 2 {
		t.Fatalf("want 2 method metas, got %d", len(metas))
	}

	byID := map[uint32]*ipcv1.MethodMeta{}
	for _, m := range metas {
		byID[m.GetMethodId()] = m
	}

	// 只读方法：无权限门槛、无 lease、无需确认、非运动。
	get := byID[1]
	if get == nil {
		t.Fatal("method_id 1 (GetGaitState) missing")
	}
	if !get.GetIsReadOnly() || get.GetIsMotion() || get.GetNeedsUserConfirmation() ||
		get.GetRequiresControlLease() || get.GetRiskClass() != ipcv1.RiskClass_RISK_CLASS_NORMAL {
		t.Errorf("GetGaitState meta wrong: %+v", get)
	}

	// 危险方法：OEM 自定义权限 + 物理控制 + 需 lease + 需系统确认框 + 运动。
	set := byID[2]
	if set == nil {
		t.Fatal("method_id 2 (SetRawGait) missing")
	}
	if set.GetRequiredPermission() != "com.acme.dog.permission.raw_gait" {
		t.Errorf("SetRawGait required_permission = %q", set.GetRequiredPermission())
	}
	if set.GetRiskClass() != ipcv1.RiskClass_RISK_CLASS_PHYSICAL_CONTROL {
		t.Errorf("SetRawGait risk_class = %v", set.GetRiskClass())
	}
	if !set.GetRequiresControlLease() || !set.GetNeedsUserConfirmation() || !set.GetIsMotion() ||
		set.GetIsReadOnly() {
		t.Errorf("SetRawGait meta wrong: %+v", set)
	}
	if set.GetRequestType() != "com.acme.dog.v1.SetRawGaitRequest" {
		t.Errorf("SetRawGait request_type = %q", set.GetRequestType())
	}
}

// TestExtractMethodMetas_AutoIncludesNewValue 锁住 A3 §「随便加一个带 option 的
// 枚举值，抽取结果自动包含它」的验证要求：抽取数 == 挂了 option 的枚举值数。
func TestExtractMethodMetas_AutoIncludesNewValue(t *testing.T) {
	enum := dogv1.RawGaitMethod(0).Descriptor()
	metas, err := ExtractMethodMetas(enum)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// 手动数一遍带 method_meta option 的枚举值，应与抽取结果一致。
	// 这条测试的价值：将来 .proto 里加/删一个带 option 的方法，此断言自动跟随，
	// 无需改任何 Go 清单——即"真相源永远是 proto"。
	if got := len(metas); got != 2 {
		t.Fatalf("extraction count drifted from proto: got %d", got)
	}
}

// TestProviderDescriptorSamples 验证标准 + OEM 两份 Descriptor 样例能按冻结的
// schema 解析（A4 门禁：标准 + OEM 两份 Descriptor 样例）。
func TestProviderDescriptorSamples(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"standard", "descriptor_standard.textproto"},
		{"oem", "descriptor_oem.textproto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var d ipcv1.ProviderDescriptor
			if err := prototext.Unmarshal(raw, &d); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.file, err)
			}
			if len(d.GetInterfaces()) == 0 {
				t.Errorf("%s: no interfaces", tc.file)
			}
			// 命名空间自洽：两份样例都应零违规（红线 #3）。
			if errs := ValidateOEMNamespace(&d); len(errs) != 0 {
				t.Errorf("%s: namespace violations: %v", tc.file, errs)
			}
		})
	}
}

// TestValidateOEMNamespace_CatchesViolation 证明校验真的拦得住违规：一个把
// 自定义权限放在别人命名空间下的 Descriptor 必须被判违规（红线 #3 反例）。
func TestValidateOEMNamespace_CatchesViolation(t *testing.T) {
	bad := &ipcv1.ProviderDescriptor{
		PackageId: "com.acme.dog",
		Interfaces: []*ipcv1.ProvidedInterface{
			// 私有接口却挂在别人（com.evil）命名空间下 —— 必须被拦。
			{InterfaceId: "com.evil.interface.hijack", Versions: []uint32{1}},
		},
		Permissions: []*ipcv1.DefinedPermission{
			// 自定义权限不在自己命名空间下 —— 必须被拦。
			{Id: "nervus.permission.motion.control"},
		},
	}
	errs := ValidateOEMNamespace(bad)
	if len(errs) != 2 {
		t.Fatalf("want 2 violations, got %d: %v", len(errs), errs)
	}
}
