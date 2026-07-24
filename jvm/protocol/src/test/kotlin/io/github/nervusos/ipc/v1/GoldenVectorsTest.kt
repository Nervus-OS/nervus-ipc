package io.github.nervusos.ipc.v1

import com.google.protobuf.ByteString
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.DynamicTest
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.TestFactory
import java.io.File

/**
 * Go↔JVM golden vectors 的 JVM 侧断言（NRCP §22.6、§10.12 结尾）。
 *
 * 每个向量在 Kotlin 里【独立构造】出与 Go 侧相同的消息，`toByteArray()` 后
 * 必须逐字节等于 committed 的 testdata/golden/<name>.binpb —— 那份文件由 Go 侧
 * 的 `go test -run TestGoldenUpdate -update` 生成。两侧都断言"自己序列化的字节
 * == 同一份 committed 文件"，于是 Go 与 JVM 传递地逐字节一致。
 *
 * committed .binpb 是唯一真源；本测试【不】写盘。若两侧对不上，就是 schema 或
 * 某一侧构造漂移了，必须在这里挡下。
 */
class GoldenVectorsTest {

    private val goldenDir: File by lazy {
        var dir = File(System.getProperty("user.dir")).absoluteFile
        while (true) {
            if (File(dir, "buf.yaml").exists()) {
                return@lazy File(dir, "testdata/golden")
            }
            dir = dir.parentFile ?: error("repo root (buf.yaml) not found above ${System.getProperty("user.dir")}")
        }
        @Suppress("UNREACHABLE_CODE")
        error("unreachable")
    }

    private fun bytesOf(vararg b: Int): ByteString =
        ByteString.copyFrom(ByteArray(b.size) { b[it].toByte() })

    /** name → 该向量在 Kotlin 侧构造出的字节。与 Go golden.Vectors() 一一对应。 */
    private fun build(name: String): ByteArray = when (name) {
        "response_success_ok" ->
            Envelope.newBuilder().setResponse(
                Response.newBuilder().setRequestId(1).setSuccess(
                    Success.newBuilder()
                        .setCode(StatusCode.STATUS_CODE_OK)
                        .setPayload(ByteString.copyFromUtf8("pong"))
                )
            ).build().toByteArray()

        "response_failure_permission_denied" ->
            Envelope.newBuilder().setResponse(
                Response.newBuilder().setRequestId(2).setFailure(
                    Failure.newBuilder()
                        .setCode(StatusCode.STATUS_CODE_PERMISSION_DENIED)
                        .setPublicMessage("denied")
                )
            ).build().toByteArray()

        "response_failure_unavailable" ->
            Envelope.newBuilder().setResponse(
                Response.newBuilder().setRequestId(3).setFailure(
                    Failure.newBuilder().setCode(StatusCode.STATUS_CODE_UNAVAILABLE)
                )
            ).build().toByteArray()

        "resolve_failure_interface_not_found" ->
            Envelope.newBuilder().setResolveEndpointResult(
                ResolveEndpointResult.newBuilder().setRequestId(4).setFailure(
                    Failure.newBuilder()
                        .setCode(StatusCode.STATUS_CODE_NOT_FOUND)
                        .setErrorDetail(
                            ResolveEndpointErrorDetail.newBuilder()
                                .setReason(ResolveEndpointReason.RESOLVE_ENDPOINT_REASON_INTERFACE_NOT_FOUND)
                                .build().toByteString()
                        )
                )
            ).build().toByteArray()

        "resolve_failure_version_mismatch" ->
            Envelope.newBuilder().setResolveEndpointResult(
                ResolveEndpointResult.newBuilder().setRequestId(5).setFailure(
                    Failure.newBuilder()
                        .setCode(StatusCode.STATUS_CODE_FAILED_PRECONDITION)
                        .setErrorDetail(
                            ResolveEndpointErrorDetail.newBuilder()
                                .setReason(ResolveEndpointReason.RESOLVE_ENDPOINT_REASON_VERSION_MISMATCH)
                                .build().toByteString()
                        )
                )
            ).build().toByteArray()

        "resolve_failure_unknown_reason" ->
            Envelope.newBuilder().setResolveEndpointResult(
                ResolveEndpointResult.newBuilder().setRequestId(6).setFailure(
                    Failure.newBuilder()
                        .setCode(StatusCode.STATUS_CODE_NOT_FOUND)
                        .setErrorDetail(
                            // reason=99：未知枚举值，用 setReasonValue 保留原始整数
                            ResolveEndpointErrorDetail.newBuilder()
                                .setReasonValue(99)
                                .build().toByteString()
                        )
                )
            ).build().toByteArray()

        "failure_malformed_detail_internal" ->
            Envelope.newBuilder().setResponse(
                Response.newBuilder().setRequestId(7).setFailure(
                    Failure.newBuilder()
                        .setCode(StatusCode.STATUS_CODE_INTERNAL)
                        .setErrorDetail(bytesOf(0xFF, 0xFF, 0xFF, 0xFF, 0x0F))
                )
            ).build().toByteArray()

        "acquire_control_human_base" ->
            Envelope.newBuilder().setAcquireControl(
                AcquireControl.newBuilder()
                    .setRequestId(8)
                    .setControllerClass(ControllerClass.CONTROLLER_CLASS_HUMAN)
                    .setResource(
                        ResourceSelector.newBuilder()
                            .setType("nervus.resource.motion.base").setRole("main")
                    )
                    .setRequestedDeadlineNanos(500_000_000)
            ).build().toByteArray()

        "acquire_control_ai_arm" ->
            Envelope.newBuilder().setAcquireControl(
                AcquireControl.newBuilder()
                    .setRequestId(9)
                    .setControllerClass(ControllerClass.CONTROLLER_CLASS_AI)
                    .setResource(
                        ResourceSelector.newBuilder()
                            .setType("nervus.resource.manipulator").setRole("main")
                    )
                    .setRequestedDeadlineNanos(1_000_000_000)
            ).build().toByteArray()

        "acquire_control_result_success" ->
            Envelope.newBuilder().setAcquireControlResult(
                AcquireControlResult.newBuilder().setRequestId(10).setSuccess(
                    AcquireControlSuccess.newBuilder()
                        .setLeaseId(0xABCD)
                        .setMotionEpoch(3)
                        .setDeadlineNanos(1_234_567_890)
                        .setResourceHandle("base.main")
                )
            ).build().toByteArray()

        "acquire_control_result_held_by_human" ->
            Envelope.newBuilder().setAcquireControlResult(
                AcquireControlResult.newBuilder().setRequestId(11).setFailure(
                    Failure.newBuilder()
                        .setCode(StatusCode.STATUS_CODE_FAILED_PRECONDITION)
                        .setErrorDetail(
                            ControlLeaseErrorDetail.newBuilder()
                                .setReason(ControlLeaseErrorReason.CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN)
                                .build().toByteString()
                        )
                )
            ).build().toByteArray()

        "acquire_control_result_unknown_reason" ->
            Envelope.newBuilder().setAcquireControlResult(
                AcquireControlResult.newBuilder().setRequestId(12).setFailure(
                    Failure.newBuilder()
                        .setCode(StatusCode.STATUS_CODE_FAILED_PRECONDITION)
                        .setErrorDetail(
                            ControlLeaseErrorDetail.newBuilder()
                                .setReasonValue(99)
                                .build().toByteString()
                        )
                )
            ).build().toByteArray()

        "release_control" ->
            Envelope.newBuilder().setReleaseControl(
                ReleaseControl.newBuilder().setRequestId(13).setLeaseId(0xABCD)
            ).build().toByteArray()

        "release_control_result_success" ->
            Envelope.newBuilder().setReleaseControlResult(
                ReleaseControlResult.newBuilder().setRequestId(14).setSuccess(
                    Success.newBuilder().setCode(StatusCode.STATUS_CODE_OK)
                )
            ).build().toByteArray()

        "interface_schema_bundle" ->
            InterfaceSchemaBundle.newBuilder()
                .setInterfaceId("com.acme.interface.gripper")
                .setVersion(1)
                .setSchemaHash(bytesOf(0xDE, 0xAD, 0xBE, 0xEF))
                .setFileDescriptorSet(bytesOf(0x0A, 0x03, 'a'.code, 'b'.code, 'c'.code))
                .build().toByteArray()

        "interface_schema_bundle_set" ->
            InterfaceSchemaBundleSet.newBuilder()
                .addBundles(
                    InterfaceSchemaBundle.newBuilder()
                        .setInterfaceId("nervus.interface.motion.base")
                        .setVersion(1)
                        .setSchemaHash(bytesOf(0x01, 0x02, 0x03, 0x04))
                        .setFileDescriptorSet(bytesOf(0x0A, 0x01, 'x'.code))
                )
                .addBundles(
                    InterfaceSchemaBundle.newBuilder()
                        .setInterfaceId("com.acme.interface.gripper")
                        .setVersion(2)
                        .setSchemaHash(bytesOf(0xDE, 0xAD, 0xBE, 0xEF))
                        .setFileDescriptorSet(bytesOf(0x0A, 0x01, 'y'.code))
                )
                .build().toByteArray()

        else -> error("unknown golden vector: $name")
    }

    /** 从 vectors.json 里提取全部向量名（不引第三方 JSON 库，手工扫 "name" 字段）。 */
    private fun vectorNames(): List<String> {
        val text = File(goldenDir, "vectors.json").readText()
        return Regex("\"name\"\\s*:\\s*\"([^\"]+)\"").findAll(text).map { it.groupValues[1] }.toList()
    }

    @TestFactory
    fun `each vector serializes byte-identically to committed golden`(): List<DynamicTest> {
        val names = vectorNames()
        assertTrue(names.isNotEmpty(), "vectors.json has no vectors (run Go -update first)")
        return names.map { name ->
            DynamicTest.dynamicTest(name) {
                val want = File(goldenDir, "$name.binpb").readBytes()
                val got = build(name)
                assertArrayEquals(
                    want, got,
                    "JVM bytes for '$name' differ from committed golden .binpb"
                )
            }
        }
    }

    @Test
    fun `unknown reason keeps generic code and round-trips`() {
        val env = Envelope.parseFrom(File(goldenDir, "resolve_failure_unknown_reason.binpb").readBytes())
        val f = env.resolveEndpointResult.failure
        assertEquals(StatusCode.STATUS_CODE_NOT_FOUND, f.code, "generic code must survive unknown reason")
        val d = ResolveEndpointErrorDetail.parseFrom(f.errorDetail)
        assertEquals(99, d.reasonValue, "unknown reason must round-trip as raw 99")
        assertEquals(ResolveEndpointReason.UNRECOGNIZED, d.reason, "unknown enum surfaces as UNRECOGNIZED")
    }

    @Test
    fun `malformed detail carries INTERNAL`() {
        val env = Envelope.parseFrom(File(goldenDir, "failure_malformed_detail_internal.binpb").readBytes())
        assertEquals(StatusCode.STATUS_CODE_INTERNAL, env.response.failure.code)
    }

    @Test
    fun `acquire control decodes resource-scoped fields`() {
        val env = Envelope.parseFrom(File(goldenDir, "acquire_control_human_base.binpb").readBytes())
        val a = env.acquireControl
        assertEquals(ControllerClass.CONTROLLER_CLASS_HUMAN, a.controllerClass)
        assertEquals("nervus.resource.motion.base", a.resource.type)
        assertEquals("main", a.resource.role)
        assertEquals(500_000_000, a.requestedDeadlineNanos)
    }

    @Test
    fun `held-by-human is a distinguishable typed reason`() {
        val env = Envelope.parseFrom(File(goldenDir, "acquire_control_result_held_by_human.binpb").readBytes())
        val f = env.acquireControlResult.failure
        assertEquals(StatusCode.STATUS_CODE_FAILED_PRECONDITION, f.code)
        val d = ControlLeaseErrorDetail.parseFrom(f.errorDetail)
        assertEquals(ControlLeaseErrorReason.CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN, d.reason)
    }

    @Test
    fun `schema bundle decodes id and version`() {
        val b = InterfaceSchemaBundle.parseFrom(File(goldenDir, "interface_schema_bundle.binpb").readBytes())
        assertEquals("com.acme.interface.gripper", b.interfaceId)
        assertEquals(1, b.version)
    }
}
