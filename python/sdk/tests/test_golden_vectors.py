"""Go↔JVM↔**Python** golden vectors 的 Python 侧断言（NRCP §22.6、§10.12 结尾）。

每个向量在 Python 里【独立构造】出与 Go/JVM 侧相同的消息，`SerializeToString()` 后
必须逐字节等于 committed 的 `testdata/golden/<name>.binpb`——那份文件由 Go 侧
`go test -run TestGoldenUpdate -update` 生成（唯一真源）。三侧都断言「自己序列化的
字节 == 同一份 committed 文件」，于是 Go / JVM / Python 传递地逐字节一致，防三语言漂移。

本测试【不】写盘；对不上就是某侧构造漂移了，必须在这里挡下。构造逻辑与
`go/golden/golden.go`、`jvm/.../GoldenVectorsTest.kt` 一一对应。
"""

from __future__ import annotations

import json
import os

import pytest

from nervus.ipc.v1 import envelope_pb2 as ipc
from nervus.ipc.v1 import schema_pb2 as schema
from nervus.ipc.v1 import status_pb2 as status
from nervus_ipc import errors


def _golden_dir() -> str:
    here = os.path.dirname(os.path.abspath(__file__))
    d = here
    while True:
        if os.path.exists(os.path.join(d, "buf.yaml")):
            return os.path.join(d, "testdata", "golden")
        parent = os.path.dirname(d)
        if parent == d:
            raise RuntimeError("repo root (buf.yaml) not found above " + here)
        d = parent


GOLDEN_DIR = _golden_dir()

# 模块级前置：golden testdata 未生成（还没在 go/golden 跑过
# `go test -run TestGoldenUpdate -update`）时，【整个本模块】干净跳过——不能让它
# 变成收集期错误、把 client/service_host 等其它测试模块一起中断。parametrize 在
# 收集期求值，故此处 skip 必须 allow_module_level=True。
if not os.path.exists(os.path.join(GOLDEN_DIR, "vectors.json")):
    pytest.skip(
        "golden vectors 未生成 — 先在 go/golden 跑 `go test -run TestGoldenUpdate -update`",
        allow_module_level=True,
    )


def _detail(msg) -> bytes:
    return msg.SerializeToString()


def build(name: str) -> bytes:
    if name == "response_success_ok":
        return ipc.Envelope(
            response=ipc.Response(request_id=1, success=status.Success(code=status.STATUS_CODE_OK, payload=b"pong"))
        ).SerializeToString()

    if name == "response_failure_permission_denied":
        return ipc.Envelope(
            response=ipc.Response(
                request_id=2,
                failure=status.Failure(code=status.STATUS_CODE_PERMISSION_DENIED, public_message="denied"),
            )
        ).SerializeToString()

    if name == "response_failure_unavailable":
        return ipc.Envelope(
            response=ipc.Response(request_id=3, failure=status.Failure(code=status.STATUS_CODE_UNAVAILABLE))
        ).SerializeToString()

    if name == "resolve_failure_interface_not_found":
        return ipc.Envelope(
            resolve_endpoint_result=ipc.ResolveEndpointResult(
                request_id=4,
                failure=status.Failure(
                    code=status.STATUS_CODE_NOT_FOUND,
                    error_detail=_detail(
                        status.ResolveEndpointErrorDetail(
                            reason=status.RESOLVE_ENDPOINT_REASON_INTERFACE_NOT_FOUND
                        )
                    ),
                ),
            )
        ).SerializeToString()

    if name == "resolve_failure_version_mismatch":
        return ipc.Envelope(
            resolve_endpoint_result=ipc.ResolveEndpointResult(
                request_id=5,
                failure=status.Failure(
                    code=status.STATUS_CODE_FAILED_PRECONDITION,
                    error_detail=_detail(
                        status.ResolveEndpointErrorDetail(
                            reason=status.RESOLVE_ENDPOINT_REASON_VERSION_MISMATCH
                        )
                    ),
                ),
            )
        ).SerializeToString()

    if name == "resolve_failure_unknown_reason":
        d = status.ResolveEndpointErrorDetail()
        d.reason = 99  # 未知枚举值：proto3 开放枚举原样保留
        return ipc.Envelope(
            resolve_endpoint_result=ipc.ResolveEndpointResult(
                request_id=6,
                failure=status.Failure(code=status.STATUS_CODE_NOT_FOUND, error_detail=d.SerializeToString()),
            )
        ).SerializeToString()

    if name == "failure_malformed_detail_internal":
        return ipc.Envelope(
            response=ipc.Response(
                request_id=7,
                failure=status.Failure(
                    code=status.STATUS_CODE_INTERNAL,
                    error_detail=bytes([0xFF, 0xFF, 0xFF, 0xFF, 0x0F]),
                ),
            )
        ).SerializeToString()

    if name == "acquire_control_human_base":
        return ipc.Envelope(
            acquire_control=ipc.AcquireControl(
                request_id=8,
                controller_class=ipc.CONTROLLER_CLASS_HUMAN,
                resource=ipc.ResourceSelector(type="nervus.resource.motion.base", role="main"),
                requested_deadline_nanos=500_000_000,
            )
        ).SerializeToString()

    if name == "acquire_control_ai_arm":
        return ipc.Envelope(
            acquire_control=ipc.AcquireControl(
                request_id=9,
                controller_class=ipc.CONTROLLER_CLASS_AI,
                resource=ipc.ResourceSelector(type="nervus.resource.manipulator", role="main"),
                requested_deadline_nanos=1_000_000_000,
            )
        ).SerializeToString()

    if name == "acquire_control_result_success":
        return ipc.Envelope(
            acquire_control_result=ipc.AcquireControlResult(
                request_id=10,
                success=ipc.AcquireControlSuccess(
                    lease_id=0xABCD,
                    motion_epoch=3,
                    deadline_nanos=1_234_567_890,
                    resource_handle="base.main",
                ),
            )
        ).SerializeToString()

    if name == "acquire_control_result_held_by_human":
        return ipc.Envelope(
            acquire_control_result=ipc.AcquireControlResult(
                request_id=11,
                failure=status.Failure(
                    code=status.STATUS_CODE_FAILED_PRECONDITION,
                    error_detail=_detail(
                        ipc.ControlLeaseErrorDetail(
                            reason=ipc.CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN
                        )
                    ),
                ),
            )
        ).SerializeToString()

    if name == "acquire_control_result_unknown_reason":
        d = ipc.ControlLeaseErrorDetail()
        d.reason = 99
        return ipc.Envelope(
            acquire_control_result=ipc.AcquireControlResult(
                request_id=12,
                failure=status.Failure(
                    code=status.STATUS_CODE_FAILED_PRECONDITION, error_detail=d.SerializeToString()
                ),
            )
        ).SerializeToString()

    if name == "release_control":
        return ipc.Envelope(
            release_control=ipc.ReleaseControl(request_id=13, lease_id=0xABCD)
        ).SerializeToString()

    if name == "release_control_result_success":
        return ipc.Envelope(
            release_control_result=ipc.ReleaseControlResult(
                request_id=14, success=status.Success(code=status.STATUS_CODE_OK)
            )
        ).SerializeToString()

    if name == "interface_schema_bundle":
        return schema.InterfaceSchemaBundle(
            interface_id="com.acme.interface.gripper",
            version=1,
            schema_hash=bytes([0xDE, 0xAD, 0xBE, 0xEF]),
            file_descriptor_set=bytes([0x0A, 0x03, ord("a"), ord("b"), ord("c")]),
        ).SerializeToString()

    if name == "interface_schema_bundle_set":
        return schema.InterfaceSchemaBundleSet(
            bundles=[
                schema.InterfaceSchemaBundle(
                    interface_id="nervus.interface.motion.base",
                    version=1,
                    schema_hash=bytes([0x01, 0x02, 0x03, 0x04]),
                    file_descriptor_set=bytes([0x0A, 0x01, ord("x")]),
                ),
                schema.InterfaceSchemaBundle(
                    interface_id="com.acme.interface.gripper",
                    version=2,
                    schema_hash=bytes([0xDE, 0xAD, 0xBE, 0xEF]),
                    file_descriptor_set=bytes([0x0A, 0x01, ord("y")]),
                ),
            ]
        ).SerializeToString()

    raise AssertionError(f"unknown golden vector: {name}")


def _vector_names():
    path = os.path.join(GOLDEN_DIR, "vectors.json")
    if not os.path.exists(path):
        pytest.skip(f"{path} missing — run Go `go test -run TestGoldenUpdate -update` first")
    with open(path, "r", encoding="utf-8") as f:
        manifest = json.load(f)
    return [entry["name"] for entry in manifest]


@pytest.mark.parametrize("name", _vector_names())
def test_vector_serializes_byte_identically(name):
    want_path = os.path.join(GOLDEN_DIR, name + ".binpb")
    with open(want_path, "rb") as f:
        want = f.read()
    got = build(name)
    assert got == want, f"Python bytes for {name!r} differ from committed golden .binpb"


def _read(name: str) -> bytes:
    with open(os.path.join(GOLDEN_DIR, name + ".binpb"), "rb") as f:
        return f.read()


def test_unknown_reason_keeps_generic_code_and_round_trips():
    if not os.path.exists(os.path.join(GOLDEN_DIR, "resolve_failure_unknown_reason.binpb")):
        pytest.skip("golden not generated")
    env = ipc.Envelope()
    env.ParseFromString(_read("resolve_failure_unknown_reason"))
    f = env.resolve_endpoint_result.failure
    assert f.code == status.STATUS_CODE_NOT_FOUND  # 通用 code 必须存活
    reason = errors.typed_reason(f.error_detail, status.ResolveEndpointErrorDetail)
    assert reason == 99  # 未知 reason 原样透出，不判协议损坏


def test_malformed_detail_is_internal_and_typed_reason_none():
    if not os.path.exists(os.path.join(GOLDEN_DIR, "failure_malformed_detail_internal.binpb")):
        pytest.skip("golden not generated")
    env = ipc.Envelope()
    env.ParseFromString(_read("failure_malformed_detail_internal"))
    assert env.response.failure.code == status.STATUS_CODE_INTERNAL
    # 畸形 detail 解不开 → None，回退通用 code，不抛异常
    assert errors.typed_reason(env.response.failure.error_detail, status.ResolveEndpointErrorDetail) is None


def test_held_by_human_is_distinguishable_typed_reason():
    if not os.path.exists(os.path.join(GOLDEN_DIR, "acquire_control_result_held_by_human.binpb")):
        pytest.skip("golden not generated")
    env = ipc.Envelope()
    env.ParseFromString(_read("acquire_control_result_held_by_human"))
    f = env.acquire_control_result.failure
    assert f.code == status.STATUS_CODE_FAILED_PRECONDITION
    reason = errors.typed_reason(f.error_detail, ipc.ControlLeaseErrorDetail)
    assert reason == ipc.CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN
