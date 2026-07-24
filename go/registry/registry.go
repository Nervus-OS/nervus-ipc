// Package registry 从生成的 protobuf descriptor 中**抽取** Method Registry，
// 并提供 Provider Descriptor 的命名空间绑定校验辅助。
//
// 它是 WP-A4「数据驱动、不写死」的可执行证据（`00` 红线 #4）：method 的
// 权限/风险/是否需确认等元数据是 proto 里挂在 method_id 枚举值上的 method_meta
// option（method_registry.proto），本包只**读**这些 descriptor、不另写任何清单。
// 因此新增一个方法或一个 OEM 接口，只需在其 .proto 挂 option 重新 buf generate，
// 抽取结果自动包含它——nervud 侧的 Method Registry / endpoint 接口目录随之更新，
// **内核 Go 代码一行不改**。
//
// 真相源方向（Agent 文档 §4.3）：Agent 工具元数据只来自这里抽取出的、随签名
// 分发并被 nervud 验证过的 descriptor；Provider 运行期**不能自报调低**。
package registry

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	ipcv1 "github.com/nervus-os/nervus-ipc/go/protocol/ipcv1"
)

// ExtractMethodMetas 遍历一个 method_id 枚举的全部枚举值，读出挂在其上的
// method_meta option，返回该接口的 method 级目录。没有挂 option 的值（如
// *_UNSPECIFIED = 0）被跳过。
//
// 抽取逻辑对标准接口与 OEM 接口完全一致——它只认 protobuf 的通用扩展机制，
// 不认接口名字里有没有 "acme"。这正是「加接口不改内核」成立的原因。
func ExtractMethodMetas(enum protoreflect.EnumDescriptor) ([]*ipcv1.MethodMeta, error) {
	var out []*ipcv1.MethodMeta
	values := enum.Values()
	for i := 0; i < values.Len(); i++ {
		val := values.Get(i)
		opts := val.Options()
		if opts == nil || !proto.HasExtension(opts, ipcv1.E_MethodMeta) {
			continue
		}
		mm, ok := proto.GetExtension(opts, ipcv1.E_MethodMeta).(*ipcv1.MethodMeta)
		if !ok || mm == nil {
			continue
		}
		// 约定：枚举值编号即 method_id。二者不一致说明 .proto 写错了，
		// 抽取阶段就 fail closed，别让漂移流进 dispatch。
		if uint32(val.Number()) != mm.GetMethodId() {
			return nil, fmt.Errorf(
				"registry: enum value %q number %d != method_meta.method_id %d",
				val.Name(), val.Number(), mm.GetMethodId())
		}
		out = append(out, mm)
	}
	return out, nil
}

// ValidateOEMNamespace 是 nervud §7.3 独立校验里「命名空间绑定」那一条的可执行
// 参考实现（`00` 红线 #3：OEM 私有接口/权限/资源类型必须在定义者命名空间下）。
//
// 它**不**做签名/信任 profile/最低保护级校验——那些是 nervud 的活，需要
// 内核持有的签名与策略状态。这里只覆盖纯粹从 Descriptor 本身即可判定的一条：
// 凡是落在 nervus.* 之外（即 OEM 私有）的 id，必须以 package_id 命名空间为前缀。
//
// 返回全部违规项；空 slice 表示命名空间自洽。
func ValidateOEMNamespace(d *ipcv1.ProviderDescriptor) []error {
	var errs []error
	ns := d.GetPackageId()

	isStd := func(id string) bool { return strings.HasPrefix(id, "nervus.") }
	underNS := func(id string) bool {
		return id == ns || strings.HasPrefix(id, ns+".")
	}

	for _, iface := range d.GetInterfaces() {
		id := iface.GetInterfaceId()
		if !isStd(id) && !underNS(id) {
			errs = append(errs, fmt.Errorf(
				"interface_id %q is private but not under package namespace %q", id, ns))
		}
	}
	for _, res := range d.GetResources() {
		rt := res.GetResourceType()
		if !isStd(rt) && !underNS(rt) {
			errs = append(errs, fmt.Errorf(
				"resource_type %q is private but not under package namespace %q", rt, ns))
		}
	}
	for _, perm := range d.GetPermissions() {
		// 自定义权限**必须**在定义者命名空间下（NRCP §9.2，无标准豁免）。
		if !underNS(perm.GetId()) {
			errs = append(errs, fmt.Errorf(
				"defined permission %q not under package namespace %q", perm.GetId(), ns))
		}
	}
	return errs
}
