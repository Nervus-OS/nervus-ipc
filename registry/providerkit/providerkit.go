// Package providerkit 为【纯接口导出型】的包生成 Provider 契约产物
// (provider.binpb + schemas.binpb), 并把它们 committed 进本仓库供 JVM 侧打包链取用.
//
// # 它补的是什么缺口
//
// nervud 的 catalog builder 有一条硬要求 (internal/catalog/builder.go):
//
//	非内核 source 只要 manifest 里有 exports, 就必须同时提供 ProviderArtifacts,
//	否则整个 Catalog 构建失败 —— 那个包在开机扫描时被隔离.
//
// Go 写的服务各有自己的 providergen (pkgmanagerd / camerad) 来产出这两个文件.
// 但 Kotlin 写的系统应用【产不出来】: 产物必须是 Go 的 Deterministic protobuf
// 编码, 而 protobuf-java 不保证与它逐字节相同 —— 与 stdinterface 那份 schema
// hash 表同一个问题, 只是更麻烦一层: schema hash 是全平台通用的常量, 而
// descriptor 是 per-package 的 (含 package_id 与各接口的门槛声明).
//
// 于是 Kotlin 应用一旦想导出接口, 就卡死在这里. permissionui 要实现
// nervus.interface.permission.ui 正是这个现场.
//
// 本包让 Go 侧生成一次, 落盘成提交进仓库的产物, JVM 侧只拷不算 —— 与
// stdinterface 同一形态 (Go 构造 -> committed 产物 -> 门禁钉住不漂移).
//
// # 边界: 只做"纯接口导出型"
//
// 即零 ManagedResource, 零自定义权限的包. 那覆盖了 permissionui 以及将来大多数
// Kotlin 系统应用 —— 它们导出的是软件接口, 不占用物理设备.
//
// camerad 那种【不】收进来: 它的资源集合要读板级 JSON, 一块板上有几路摄像头是
// 板级事实而不是代码常量. 硬把两者统一, 等于让本包去理解板级配置, 而那份逻辑
// 已经在 camerad 自己的 providergen 里, 并且必须与运行期读同一份文件.
//
// 自定义权限同理不收: 定义一条权限要给出中英文案与 GrantMode / RiskClass /
// MinimumTrust 三档门槛, 那是需要逐条评审的东西, 不该藏在一个"顺手生成"的表里.
//
// # 一处必须写对的地方
//
// 若某个接口【内核 bootstrap 里也定义了】(permission.ui 就是), 本包声明的那份
// 必须与内核那份逐字一致: catalog 的 sameInterfaceContract 会对两份定义做
// reflect.DeepEqual 加每条 MethodMeta 的 proto.Equal, 不一致则拒绝第二个 Provider.
//
// 写歪一个字段的症状是"两家明明写了一样的接口却被内核判成冲突", 而且只在真机
// 开机扫描时暴露 —— 那时镜像已经烧好. nervud 侧另有一道契约测试钉住这件事.
package providerkit

import (
	"errors"
	"fmt"
	"path"
	"sort"

	permissionv1 "github.com/nervus-os/nervus-ipc/protocol/interface/permissionv1"
	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nervus-os/nervus-ipc/registry"
)

// 产物文件名. 与 nervud pkgregistry 的 ProviderArtifactsRef 以及
// nervus-system-server 各 providergen 用的名字一致 —— manifest 里写的就是这两个名字.
const (
	DescriptorFileName = "provider.binpb"
	SchemasFileName    = "schemas.binpb"
)

// ResourceRoot 是 committed 产物在本仓库中的根目录.
//
// 放在 jvm/protocol/src/main/resources 之下, 是为了走 nervus-app-sdk 的
// cloneProtocol 那条已有管道: 它拷 jvm/protocol/src/main 的全部内容, 因此产物
// 随 SDK jar 发布, 而 pin 一变产物跟着变 —— 不会出现"pin 更新了但 binpb 还是
// 旧的"这种最难查的错配.
//
// 也刻意不放 java/: buf generate 的 clean 会整删那个目录 (它是 protoc 的输出),
// 手写或另行生成的文件放进去会在下一次重新生成时消失.
const ResourceRoot = "jvm/protocol/src/main/resources/provider"

// Interface 是一个被导出的接口声明.
//
// 字段与 ipcv1.ProvidedInterface 一一对应, 但只暴露"纯接口导出型"用得到的那几个:
// 资源相关的三个字段 (compatible_resource_types / default_resource_type /
// default_resource_role) 一律留空, 见包注释的边界说明.
type Interface struct {
	// ID 是 interface_id. 标准接口用 nervus.* , 私有接口必须位于本包命名空间下
	// (registry.ValidateOEMNamespace 会拦).
	ID string

	// Major 是接口 major 版本. 0 是保留值.
	Major uint32

	// RequiredPermission 是【接口级】门槛, 即"能不能 Resolve 到它".
	//
	// 若内核 bootstrap 也定义了同一个接口, 这里必须与那份逐字一致 ——
	// 它是 sameInterfaceContract 的比对对象之一.
	//
	// 留空表示不设接口级门槛 (方法级 required_permission 仍然生效).
	RequiredPermission string

	// MethodEnum 是挂了 method_meta 的枚举描述符.
	//
	// 直接取生成代码的枚举而不是写字面量: 枚举改了名或换了文件, 这里编译不过;
	// 写字面量则只会在运行期表现为一个含义模糊的 hash 不符.
	MethodEnum protoreflect.EnumDescriptor

	// EventEnum 是挂了 event_meta 的枚举描述符; 不推送事件的接口留 nil.
	EventEnum protoreflect.EnumDescriptor
}

// Spec 是一个包的 Provider 契约声明.
type Spec struct {
	// PackageID 必须与 manifest 的 package_id 一致 —— nervud 的
	// loadRequiredProviderArtifacts 会比对两者, 不符即拒装载.
	PackageID string

	// Interfaces 是本包导出的全部接口.
	//
	// 【每一个都必须同时出现在 manifest 某个 component 的 exports 里】:
	// catalog 的 addArtifacts 是双向闭合的 —— descriptor 里有而 exports 里没有,
	// 或反之, 两种都拒.
	Interfaces []Interface
}

// All 返回全部有 committed 产物的包, 顺序稳定.
//
// 新包一律追加到末尾. 这份清单与 nervud internal/catalog 的 bootstrap 是两个
// 独立的事实源, 因此对【内核也定义了的接口】必须逐字一致 —— nervud 侧有一道
// 契约测试钉住它 (见包注释).
//
// permission.ui 的三项取值来自 nervud bootstrap 里那次 bootstrapInterface 调用:
//
//	required_permission = "perm.pkg.query"   接口级门槛, 即"能不能 Resolve 到它"
//	risk_class          = UNSPECIFIED        不绑物理资源
//	资源三项             = 空
//
// 特别注意 required_permission 【不是】 perm.permission.admin: 那条是
// permission.admin (内核内建的授权面) 的门槛. 本接口是"请求向用户展示确认",
// 调用方是设置与桌面, 它们持有 perm.pkg.query 而不该持有授权面的钥匙.
func All() []Spec {
	return []Spec{
		{
			PackageID: "nervus.permissionui",
			Interfaces: []Interface{{
				ID:                 "nervus.interface.permission.ui",
				Major:              1,
				RequiredPermission: "perm.pkg.query",
				MethodEnum:         permissionv1.PermissionUiMethod(0).Descriptor(),
			}},
		},
	}
}

// Artifacts 是一个包生成出来的两份确定性字节.
type Artifacts struct {
	PackageID      string
	DescriptorWire []byte
	SchemaWire     []byte
}

// DescriptorPath 与 SchemasPath 返回产物在仓库中的相对路径.
func (a Artifacts) DescriptorPath() string {
	return path.Join(ResourceRoot, a.PackageID, DescriptorFileName)
}

func (a Artifacts) SchemasPath() string {
	return path.Join(ResourceRoot, a.PackageID, SchemasFileName)
}

// Build 从 spec 生成两份确定性 protobuf 字节.
//
// 走 registry.BuildSchemaBundleWithEvents 与 registry.MarshalProviderArtifacts ——
// 与 nervud 的 bootstrap 以及各 providergen 是【同一批构造函数】. 那是多方
// 不漂移的唯一保证: 任何一边改用别的算法都会立刻表现为 hash 不符或契约冲突.
//
// MarshalProviderArtifacts 自己会在编码后立刻 Parse 回来校验一遍, 因此本函数
// 返回成功即意味着这两份字节能通过内核装载时的同一套校验 —— 打包期就挡下,
// 好过等到目标机扫描时才发现, 那时镜像已经烧好.
func Build(spec Spec) (Artifacts, error) {
	if spec.PackageID == "" {
		return Artifacts{}, errors.New("providerkit: empty package id")
	}
	if len(spec.Interfaces) == 0 {
		// 一个不导出任何接口的包根本不需要 ProviderArtifacts. 生成一份空的
		// 只会让 manifest 里多出一个 provider 段, 而内核会因"descriptor 没有
		// 接口"之类的理由拒绝装载 —— 那个错误离"其实你不需要它"很远.
		return Artifacts{}, fmt.Errorf(
			"providerkit: package %q declares no interfaces; omit the provider section instead",
			spec.PackageID)
	}

	descriptor := &ipcv1.ProviderDescriptor{
		PackageId:  spec.PackageID,
		Interfaces: make([]*ipcv1.ProvidedInterface, 0, len(spec.Interfaces)),
		// 不声明 Resources 与 Permissions: 见包注释的边界说明
	}
	bundles := &ipcv1.InterfaceSchemaBundleSet{
		Bundles: make([]*ipcv1.InterfaceSchemaBundle, 0, len(spec.Interfaces)),
	}

	seen := make(map[string]struct{}, len(spec.Interfaces))
	for _, iface := range spec.Interfaces {
		if iface.ID == "" {
			return Artifacts{}, fmt.Errorf("providerkit: package %q has an interface with empty id",
				spec.PackageID)
		}
		// 同一个 interface_id 出现两次会让 descriptor 里有两条冲突记录, 而读的
		// 一方取到哪条取决于它怎么遍历. registry 的校验也会拒, 但在这里报错
		// 能直接指向 spec 里那一行.
		if _, dup := seen[iface.ID]; dup {
			return Artifacts{}, fmt.Errorf("providerkit: package %q declares interface %q twice",
				spec.PackageID, iface.ID)
		}
		seen[iface.ID] = struct{}{}

		bundle, err := registry.BuildSchemaBundleWithEvents(
			iface.ID, iface.Major, iface.MethodEnum, iface.EventEnum)
		if err != nil {
			return Artifacts{}, fmt.Errorf("providerkit: package %q interface %q: %w",
				spec.PackageID, iface.ID, err)
		}
		bundles.Bundles = append(bundles.Bundles, bundle)

		descriptor.Interfaces = append(descriptor.Interfaces, &ipcv1.ProvidedInterface{
			InterfaceId: iface.ID,
			InterfaceVersions: []*ipcv1.ProvidedInterfaceVersion{{
				Major: iface.Major,
				// schema_hash 取 bundle 的 hash, 而不是 MethodsHash: methods 字段
				// 留空表示这【不是】元数据接口 —— 它有完整的 descriptor set,
				// 因此 request/response 类型能被解析校验.
				SchemaHash: append([]byte(nil), bundle.GetSchemaHash()...),
			}},
			RequiredPermission: iface.RequiredPermission,
			// 资源三项与 risk floor 全部留零值: 纯接口导出型不绑物理资源.
			// 注意 resource_risk_floor 必须是 UNSPECIFIED —— registry 的校验
			// 要求"声明了 compatible_resource_types 就必须有 risk floor",
			// 反过来没有资源时留 UNSPECIFIED 才是对的.
		})
	}

	// 两个切片都按 interface_id 排序落盘.
	//
	// Deterministic 编码只保证字段顺序确定, 不会替我们排 repeated 元素 ——
	// 那意味着调整 spec 里的书写顺序会产出一份内容等价但字节不同的产物,
	// 于是 digest 变了, 门禁红了, 而没有任何东西真的改变.
	sort.Slice(descriptor.Interfaces, func(i, j int) bool {
		return descriptor.Interfaces[i].GetInterfaceId() < descriptor.Interfaces[j].GetInterfaceId()
	})
	sort.Slice(bundles.Bundles, func(i, j int) bool {
		return bundles.Bundles[i].GetInterfaceId() < bundles.Bundles[j].GetInterfaceId()
	})

	descriptorWire, schemaWire, err := registry.MarshalProviderArtifacts(descriptor, bundles)
	if err != nil {
		return Artifacts{}, fmt.Errorf("providerkit: package %q: %w", spec.PackageID, err)
	}
	return Artifacts{
		PackageID:      spec.PackageID,
		DescriptorWire: descriptorWire,
		SchemaWire:     schemaWire,
	}, nil
}
