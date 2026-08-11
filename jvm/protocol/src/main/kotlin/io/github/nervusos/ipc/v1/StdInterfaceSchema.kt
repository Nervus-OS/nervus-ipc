// 本文件由 registry/stdinterface 生成, 【不要手改】.
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
        "nervus.interface.camera.config@1" to
            "e00e26c597b1604c4fefdfaa8e4ab4a579ccef4483d1553c4df96d7f641d7642",
        "nervus.interface.camera@1" to
            "e00e26c597b1604c4fefdfaa8e4ab4a579ccef4483d1553c4df96d7f641d7642",
        "nervus.interface.manipulator.arm@1" to
            "e30335f462edd2f1d8d3d0fb9556909595b5a3ce60fa2fb0320a29a27b6346e8",
        "nervus.interface.motion.base@1" to
            "c7ed417b4aab241b952e38f1293a827dcb62d9634f7ab0ffcf99cf5c208a47f5",
        "nervus.interface.operation.control@1" to
            "57d93305011f8faac691eb73727336d276e967c47c3edef6c93daba9db2cd569",
        "nervus.interface.permission.admin@1" to
            "9437a5ba8cc90b5a5ad1dfbd22dd525f948ef4613fb63e1fc25689cae22bdc1e",
        "nervus.interface.permission.self@1" to
            "a1078c890ff7accb4072899f41989e3a6d1b6d11f73171a5f5f60fbb67ae2ab0",
        "nervus.interface.permission.ui@1" to
            "8376df1536936b55c6ae1f50856120383581a6d4dd3375a8efc6aa1cd7a9553f",
        "nervus.interface.pkg.manager@1" to
            "6f5452d045eee4c899fdd7a005427cb5d45879d89e1a5c025836bd6b2593b64a",
        "nervus.interface.resource.directory@1" to
            "1825327bc61345aed2761aa91005b18997e4cc34dbd7eb56190e05d53dd3cc4a",
        "nervus.interface.safety.control@1" to
            "bbcb223b5d127ea6b6ead9a2784c733f9f35280db46e5e9b0013d12b46fd148c",
        "nervus.interface.transfer.control@1" to
            "57117b1777cb1998cf10adfb8401d8bc6cb105b4c0e1e8c5eee118cdac276497",
    )

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
