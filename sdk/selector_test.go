package sdk

import (
	"testing"

	"google.golang.org/protobuf/proto"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// 没给 Selector 时沿用 type/role 两个便捷字段。
func TestResolveRequest_SelectorFallsBackToTypeRole(t *testing.T) {
	req := ResolveRequest{
		InterfaceID:  "nervus.interface.motion.base",
		ResourceType: "nervus.resource.motion.base",
		ResourceRole: "main",
	}
	got := req.selector()
	want := &ipcv1.ResourceSelector{
		Type: "nervus.resource.motion.base", Role: "main",
	}
	if !proto.Equal(got, want) {
		t.Fatalf("selector = %+v, want %+v", got, want)
	}
}

// 两者都空时不发 selector：空 selector 的语义（回落到接口默认资源）
// 由 nervud 决定，SDK 不该替它造一个空壳。
func TestResolveRequest_EmptyStaysNil(t *testing.T) {
	if got := (ResolveRequest{InterfaceID: "x"}).selector(); got != nil {
		t.Fatalf("selector = %+v, want nil", got)
	}
}

// 给了 Selector 就【完全取代】便捷字段——不做合并。合并会产生
// 「填了三处、最终生效的是哪个」这种要看实现才知道的语义。
func TestResolveRequest_SelectorOverridesConvenienceFields(t *testing.T) {
	explicit := &ipcv1.ResourceSelector{
		Type:   "nervus.resource.camera",
		Labels: map[string]string{"nervus.camera.facing": "front"},
		Policy: ipcv1.ResourceSelectionPolicy_RESOURCE_SELECTION_POLICY_SYSTEM_PREFERRED,
	}
	req := ResolveRequest{
		InterfaceID:  "nervus.interface.camera",
		ResourceType: "should.be.ignored",
		ResourceRole: "ignored",
		Selector:     explicit,
	}
	got := req.selector()
	if !proto.Equal(got, explicit) {
		t.Fatalf("selector = %+v, want %+v", got, explicit)
	}
	if got.GetType() == "should.be.ignored" {
		t.Fatal("便捷字段污染了显式 Selector")
	}
}

// 标签与策略必须原样传到 wire 上——这是 V2-2 能被 Go 服务用到的全部依据。
func TestResolveRequest_LabelsAndPolicySurvive(t *testing.T) {
	req := ResolveRequest{
		InterfaceID: "nervus.interface.camera",
		Selector: &ipcv1.ResourceSelector{
			Type: "nervus.resource.camera",
			Labels: map[string]string{
				"nervus.camera.facing": "front",
				"nervus.camera.class":  "4k",
			},
			Policy: ipcv1.ResourceSelectionPolicy_RESOURCE_SELECTION_POLICY_REQUIRE_UNIQUE,
		},
	}
	got := req.selector()
	if len(got.GetLabels()) != 2 ||
		got.GetLabels()["nervus.camera.facing"] != "front" ||
		got.GetLabels()["nervus.camera.class"] != "4k" {
		t.Fatalf("labels = %+v", got.GetLabels())
	}
	if got.GetPolicy() != ipcv1.ResourceSelectionPolicy_RESOURCE_SELECTION_POLICY_REQUIRE_UNIQUE {
		t.Fatalf("policy = %v", got.GetPolicy())
	}
}
