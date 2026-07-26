package registry_test

import (
	"testing"

	pkgv1 "github.com/nervus-os/nervus-ipc/protocol/interface/pkgmanagerv1"
	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
	"github.com/nervus-os/nervus-ipc/registry"
)

// 新增一个接口时，Method Registry 应当【自动】包含它——OEM/平台只在自己的
// .proto 里挂 (method_meta)，nervud 一行 Go 都不用改。
//
// 本测试就是那句话的可执行证据：PackageManager 是本轮新加的接口，抽取代码
// 一个字没动，它的四个 method 元数据就出来了。
func TestExtractMethodMetas_PackageManager(t *testing.T) {
	metas, err := registry.ExtractMethodMetas(
		pkgv1.PackageManagerMethod(0).Descriptor(),
	)
	if err != nil {
		t.Fatalf("ExtractMethodMetas: %v", err)
	}
	if len(metas) != 4 {
		t.Fatalf("抽出 %d 条 method_meta, want 4", len(metas))
	}

	byID := make(map[uint32]*ipcv1.MethodMeta, len(metas))
	for _, m := range metas {
		byID[m.GetMethodId()] = m
	}

	// Install：长任务 + 最高风险 + 必须用户确认
	install := byID[1]
	if install == nil {
		t.Fatal("缺少 method_id=1 (Install)")
	}
	if !install.GetReturnsOperation() {
		t.Error("Install 必须是 operation：解包 + 全量 digest 复核远超普通 deadline 预算")
	}
	if install.GetRiskClass() != ipcv1.RiskClass_RISK_CLASS_CRITICAL_SAFETY {
		t.Errorf("Install risk_class = %v, want CRITICAL_SAFETY（引入新可执行代码，后果持久性远超一次运动指令）",
			install.GetRiskClass())
	}
	if !install.GetNeedsUserConfirmation() {
		t.Error("Install 必须要求系统确认框：Agent 只能请求，不能自行确认")
	}
	// 装包不驱动执行器
	if install.GetIsMotion() {
		t.Error("Install is_motion 必须为 false")
	}
	if install.GetRequiresControlLease() {
		t.Error("Install 不需要 ControlLease：它不碰执行器")
	}

	// List：只读，不需要确认框
	list := byID[3]
	if list == nil {
		t.Fatal("缺少 method_id=3 (List)")
	}
	if !list.GetIsReadOnly() {
		t.Error("List 必须标记 is_read_only")
	}
	if list.GetNeedsUserConfirmation() {
		t.Error("只读查询不该弹确认框")
	}
	if list.GetRiskClass() != ipcv1.RiskClass_RISK_CLASS_NORMAL {
		t.Errorf("List risk_class = %v, want NORMAL", list.GetRiskClass())
	}

	// 全部写操作都必须要求确认
	for _, id := range []uint32{1, 2, 4} {
		if m := byID[id]; m != nil && !m.GetNeedsUserConfirmation() {
			t.Errorf("method_id=%d 是写操作，必须 needs_user_confirmation", id)
		}
	}
}
