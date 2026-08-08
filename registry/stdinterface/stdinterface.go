// Package stdinterface 是全部平台标准接口 (nervus.interface.*) 的集中清单,
// 以及它们 schema hash 的唯一构造真源.
//
// # 它补的是什么缺口
//
// RegisterEndpoint 必须带 interface_schema_hash, 内核会与 Catalog 里那份逐字节
// 比对, 不符即拒 (nervud internal/endpoint/register.go). 空值也一律拒 —— 曾经
// 有一条只放行 pkgmanagerd 空 schema 的兼容桥, 在打包链能产出 ProviderArtifacts
// 之后已经移除.
//
// Go 写的 Provider 直接调 BuildSchemaBundle 就能算出这个值 (pkgmanagerd 的
// contract.go 即如此). 但 hash 是 sha256(确定性编码的 FileDescriptorSet), 而
// JVM 侧【没有】registry 包的等价物, protobuf-java 也不保证与 Go 的
// Deterministic 编码逐字节相同. 于是任何 Kotlin/Java 写的 Provider 都算不出这个
// 值, 也就注册不了任何接口 —— nervus-app-example 的 HelloService 导出接口时
// schemaHash 留空, 按现在的规则报到必然被拒, 就是这个缺口的现场.
//
// 本包让 Go 侧算一次, 落盘成提交进仓库的 Kotlin 常量, JVM 侧只读不算. 跨语言
// 一致性由"读同一份生成物"保证, 不由"两边各实现一遍算法"保证 —— 与 golden
// vectors 同一形态 (Go 构造 -> committed 产物 -> 两侧都对同一份字节断言).
//
// # 为什么生成 Kotlin 源码而不是落一份 bundle set 二进制
//
// 直觉上该复用协议里已有的 InterfaceSchemaBundleSet, JVM 用生成好的 protobuf
// 类一行解开. 试过, 不划算:
//
//	每个 bundle 内嵌自己那份完整 FileDescriptorSet (含全部传递依赖), 11 个接口
//	共 360 KB, 而真正需要的只有 11 x 32 字节. 且大部分内容在各 bundle 间重复
//	(所有接口都 import 同一批 ipc/v1 文件).
//
// 想剥掉 descriptor set 只留 hash 更糟: ParseSchemaBundle 明确要求 descriptor
// set 非空且其 sha256 等于 schema_hash. 一个剥了的 bundle set 是"看起来是
// bundle set 但按协议解不开"的东西, 比不用这个类型更坏.
//
// Kotlin 常量则正好走通一条已经接好但还空着的管道: nervus-app-sdk 的
// cloneProtocol 拷 jvm/protocol/src/main 全部内容并把 kotlin/ 加进 source set,
// 而 buf generate 的 clean 只整删 java/ —— 手写文件放 kotlin/ 既能到达 SDK,
// 又不会被重新生成抹掉.
//
// # 漂移防线
//
// TestCommittedKotlinMatches 断言 committed 的 .kt 逐字节等于本包现算的结果.
// 改了任何标准接口的 .proto 而没重跑生成, CI 即红. 重跑命令写在生成物头部.
package stdinterface

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	basemotionv1 "github.com/nervus-os/nervus-ipc/protocol/interface/basemotionv1"
	camerav1 "github.com/nervus-os/nervus-ipc/protocol/interface/camerav1"
	manipulatorv1 "github.com/nervus-os/nervus-ipc/protocol/interface/manipulatorv1"
	operationv1 "github.com/nervus-os/nervus-ipc/protocol/interface/operationv1"
	permissionv1 "github.com/nervus-os/nervus-ipc/protocol/interface/permissionv1"
	pkgmanagerv1 "github.com/nervus-os/nervus-ipc/protocol/interface/pkgmanagerv1"
	resourcedirv1 "github.com/nervus-os/nervus-ipc/protocol/interface/resourcedirv1"
	safetyv1 "github.com/nervus-os/nervus-ipc/protocol/interface/safetyv1"
	transferv1 "github.com/nervus-os/nervus-ipc/protocol/interface/transferv1"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nervus-os/nervus-ipc/registry"
)

// KotlinTableFile 是 committed 生成物在仓库中的相对路径.
//
// 放在 jvm/protocol/src/main/kotlin 而不是 java: buf generate 的 clean 会整删
// java/ (它是 protoc 的输出目录), 手写文件放进去会在下一次重新生成时消失.
const KotlinTableFile = "jvm/protocol/src/main/kotlin/io/github/nervusos/ipc/v1/StdInterfaceSchema.kt"

// Interface 是一个平台标准接口的清单项.
type Interface struct {
	// ID 是 interface_id, 必须与 .proto 头部注释声明的那个一致.
	ID string

	// Major 是接口 major 版本. 0 是保留值.
	Major uint32

	// MethodEnum 是挂了 method_meta 的枚举描述符.
	//
	// 直接取生成代码的枚举而不是写字面量: 枚举改了名或换了文件, 这里编译不过;
	// 写字面量则只会在运行期表现为一个含义模糊的 hash 不符.
	MethodEnum protoreflect.EnumDescriptor

	// EventEnum 是挂了 event_meta 的枚举描述符; 不推送事件的接口留 nil.
	//
	// 它【不影响】schema hash: 事件枚举与方法枚举同文件, 本来就在 descriptor
	// set 里 (见 BuildSchemaBundleWithEvents 的说明). 如实填写是为了让本清单
	// 同时能当"哪些标准接口有事件"的索引用.
	EventEnum protoreflect.EnumDescriptor
}

// Key 返回 "<id>@<major>" 形式的稳定键.
func (i Interface) Key() string { return fmt.Sprintf("%s@%d", i.ID, i.Major) }

// All 返回全部平台标准接口, 顺序稳定.
//
// 新接口一律【追加到末尾】. 生成物里的条目按键排序 (与本顺序无关), 但保持这份
// 清单只增不重排, 能让 diff 只显示新增的那一行.
//
// 这份清单必须与 nervud internal/catalog 的 bootstrap 保持一致. 两边都从同一批
// 生成代码的枚举取 descriptor, 因此漂移只可能是"一边加了新接口另一边忘了",
// 不可能是同一接口算出两个 hash.
func All() []Interface {
	return []Interface{
		{
			ID:         "nervus.interface.motion.base",
			Major:      1,
			MethodEnum: basemotionv1.BaseMotionMethod(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.manipulator.arm",
			Major:      1,
			MethodEnum: manipulatorv1.ManipulatorMethod(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.pkg.manager",
			Major:      1,
			MethodEnum: pkgmanagerv1.PackageManagerMethod(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.safety.control",
			Major:      1,
			MethodEnum: safetyv1.SafetyControlMethod(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.transfer.control",
			Major:      1,
			MethodEnum: transferv1.TransferControlMethod(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.resource.directory",
			Major:      1,
			MethodEnum: resourcedirv1.ResourceDirectoryMethod(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.operation.control",
			Major:      1,
			MethodEnum: operationv1.OperationControlMethod(0).Descriptor(),
			EventEnum:  operationv1.OperationControlEvent(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.permission.admin",
			Major:      1,
			MethodEnum: permissionv1.PermissionAdminMethod(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.permission.ui",
			Major:      1,
			MethodEnum: permissionv1.PermissionUiMethod(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.camera",
			Major:      1,
			MethodEnum: camerav1.CameraMethod(0).Descriptor(),
			EventEnum:  camerav1.CameraEvent(0).Descriptor(),
		},
		{
			ID:         "nervus.interface.camera.config",
			Major:      1,
			MethodEnum: camerav1.CameraConfigMethod(0).Descriptor(),
			EventEnum:  camerav1.CameraConfigEvent(0).Descriptor(),
		},
	}
}

// Entry 是一个接口算出的 schema hash.
type Entry struct {
	ID         string
	Major      uint32
	SchemaHash []byte
}

// Key 返回 "<id>@<major>" 形式的稳定键.
func (e Entry) Key() string { return fmt.Sprintf("%s@%d", e.ID, e.Major) }

// Entries 为 All() 里每个接口算出 schema hash, 顺序【与 All() 一致】.
//
// 保持输入顺序而不是排序: 调用方 (含门禁测试) 能按下标把 Entry 与 Interface
// 对应起来. 生成物内部的排序在 RenderKotlin 里做, 那是另一件事 —— 落盘顺序
// 不该取决于清单的书写顺序, 否则重排清单会产出一个内容等价但字节不同的文件,
// 让门禁红得毫无信息量.
//
// 走 registry.BuildSchemaBundleWithEvents —— 与 nervud 的 bootstrap 和各
// Provider 的 providergen 是【同一个构造函数】. 那是三方不漂移的唯一保证:
// 任何一边改用别的算法都会立刻表现为 hash 不符.
func Entries() ([]Entry, error) {
	ifaces := All()
	out := make([]Entry, 0, len(ifaces))
	seen := make(map[string]struct{}, len(ifaces))
	for _, iface := range ifaces {
		key := iface.Key()
		if _, dup := seen[key]; dup {
			// 同一个 (id, major) 出现两次会让生成物里有两条同名常量, Kotlin 侧
			// 直接编译不过 —— 但报错指向生成物而不是清单, 在这里拦住更省事
			return nil, fmt.Errorf("stdinterface: duplicate interface %s", key)
		}
		seen[key] = struct{}{}

		bundle, err := registry.BuildSchemaBundleWithEvents(
			iface.ID, iface.Major, iface.MethodEnum, iface.EventEnum)
		if err != nil {
			return nil, fmt.Errorf("stdinterface: build %s: %w", key, err)
		}
		out = append(out, Entry{
			ID:         iface.ID,
			Major:      iface.Major,
			SchemaHash: bundle.GetSchemaHash(),
		})
	}
	return out, nil
}

// RenderKotlin 渲染 committed 的 Kotlin 源码.
//
// 用十六进制字符串而不是 byteArrayOf(...): 前者一行一条, diff 可读, 且与
// nervud 日志里 schema hash 的打印形式一致 (排查"两边不符"时能直接对眼).
// 解码成本是每个进程一次, 不在任何热路径上.
func RenderKotlin() ([]byte, error) {
	entries, err := Entries()
	if err != nil {
		return nil, err
	}
	// 按键排序落盘, 与 Entries() 的输入顺序解耦 (理由见 Entries 的说明)
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Key() < sorted[b].Key() })

	var b strings.Builder

	b.WriteString(`// 本文件由 registry/stdinterface 生成, 【不要手改】.
//
// 重新生成:
//   go test ./registry/stdinterface -run TestUpdateCommittedTable -update
//
// # 这是什么
//
// 全部平台标准接口的 schema hash. RegisterEndpoint 必须带上它, 内核会与 Catalog
// 里那份逐字节比对, 不符或留空一律拒 (nervud internal/endpoint/register.go).
//
// # 为什么 JVM 侧读常量而不是自己算
//
// hash 是 sha256(确定性编码的 FileDescriptorSet). Go 侧有 registry.BuildSchemaBundle
// 一行算出, JVM 侧没有等价物, 且 protobuf-java 不保证与 Go 的 Deterministic 编码
// 逐字节相同 —— 而这里要的正是逐字节相等. 所以由 Go 算一次落盘, 两侧读同一份
// 生成物, 与 golden vectors 同一形态.
//
// 改了任何标准接口的 .proto 而没重跑生成, nervus-ipc 的 CI 会红
// (TestCommittedTableMatches).

package io.github.nervusos.ipc.v1

/** 平台标准接口的 schema hash 表. */
object StdInterfaceSchema {

    /** "<interface_id>@<major>" -> schema hash 的十六进制. */
    private val hex: Map<String, String> = mapOf(
`)

	for _, e := range sorted {
		fmt.Fprintf(&b, "        %q to\n            %q,\n", e.Key(), hex.EncodeToString(e.SchemaHash))
	}

	b.WriteString(`    )

    /**
     * 取一个标准接口的 schema hash。
     *
     * 查不到即抛异常, 【不返回空数组】: 空 hash 会被内核拒, 症状是一句
     * "interface schema hash does not match the catalog", 离"这个接口不在表里"
     * 这个真正的原因很远. 在这里失败, 错误信息才指得准.
     */
    fun hashOf(interfaceId: String, major: Int = 1): ByteArray {
        val key = "$interfaceId@$major"
        val value = hex[key]
            ?: throw IllegalArgumentException(
                "no committed schema hash for '$key'; " +
                    "add it to registry/stdinterface in nervus-ipc and regenerate",
            )
        return ByteArray(value.length / 2) { i ->
            value.substring(i * 2, i * 2 + 2).toInt(16).toByte()
        }
    }

    /** 表里已登记的全部键, 供诊断用. */
    fun keys(): Set<String> = hex.keys
}
`)

	return []byte(b.String()), nil
}
