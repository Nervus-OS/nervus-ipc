"""错误解码：typed_reason 的 None/未知/正常路径 + Success/Failure code 不变量。"""

from __future__ import annotations

from nervus.ipc.v1 import envelope_pb2 as ipc
from nervus.ipc.v1 import status_pb2 as status
from nervus_ipc import status_name, typed_reason
from nervus_ipc import errors


def test_typed_reason_none_on_empty():
    assert typed_reason(b"", status.ResolveEndpointErrorDetail) is None


def test_typed_reason_none_on_garbage_does_not_raise():
    # 畸形 detail → None（回退通用 code），不抛异常、不判协议损坏
    assert typed_reason(b"\xff\xff\xff\xff\x0f", status.ResolveEndpointErrorDetail) is None


def test_typed_reason_known():
    d = status.ResolveEndpointErrorDetail(reason=status.RESOLVE_ENDPOINT_REASON_VERSION_MISMATCH)
    assert typed_reason(d.SerializeToString(), status.ResolveEndpointErrorDetail) == status.RESOLVE_ENDPOINT_REASON_VERSION_MISMATCH


def test_typed_reason_unknown_preserved():
    d = status.ResolveEndpointErrorDetail()
    d.reason = 99
    assert typed_reason(d.SerializeToString(), status.ResolveEndpointErrorDetail) == 99


def test_success_failure_code_invariants():
    assert errors.validate_success_code(status.STATUS_CODE_OK)
    assert errors.validate_success_code(status.STATUS_CODE_ACCEPTED)
    assert not errors.validate_success_code(status.STATUS_CODE_INTERNAL)

    assert errors.validate_failure_code(status.STATUS_CODE_INTERNAL)
    assert not errors.validate_failure_code(status.STATUS_CODE_OK)
    assert not errors.validate_failure_code(status.STATUS_CODE_UNSPECIFIED)


def test_status_name():
    assert status_name(status.STATUS_CODE_OK) == "OK"
    assert status_name(status.STATUS_CODE_PERMISSION_DENIED) == "PERMISSION_DENIED"
    assert "STATUS_CODE(" in status_name(9999)
