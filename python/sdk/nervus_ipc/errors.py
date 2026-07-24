"""错误语义：通用 StatusCode + typed error_detail 解码（NRCP §19、status.proto）。

分层（NRCP §19）：
    位置 A（StatusCode）—— 跨全部 Interface 稳定，SDK 据此决定重试/报错/断线；
    位置 B（typed error_detail）—— 随各 Interface schema 演进，回答领域细因。

解码纪律：**先映射通用 code，再按方法解 typed reason；未知 reason 保留通用 code，
不判协议损坏**（golden vector `*_unknown_reason`：reason=99 时外层 code 不变）。
"""

from __future__ import annotations

from typing import Optional

from google.protobuf.message import Message

from nervus.ipc.v1 import status_pb2 as status


class NervusError(Exception):
    """本 SDK 抛出的所有异常的基类。"""


class ProtocolViolation(NervusError):
    """对端违反了 wire 协议（零长度帧 / 超限 / 畸形 Envelope / 非法 outcome 组合）。"""


class Disconnected(NervusError):
    """连接断开：EOF 或本地关闭。断线时全部 pending 以本异常完结（§10.12）。"""


class Timeout(NervusError):
    """在 deadline 内没有等到终结响应。"""


class HandshakeRejected(NervusError):
    """Hello 握手被 nervud 拒绝（版本谈不拢 / 身份不成立）。"""

    def __init__(self, code: int, detail: str) -> None:
        self.code = code
        self.detail = detail
        super().__init__(f"handshake rejected: {status_name(code)} {detail}")


class CallFailed(NervusError):
    """一次调用返回了 Failure outcome。

    携带通用 `code`（位置 A）、`public_message`（仅供人读，不参与判断）、原始
    `error_detail` bytes（位置 B，类型由外层消息 + method schema 决定）。用
    :func:`typed_reason` 按具体 detail 类型解出 reason。
    """

    def __init__(self, code: int, public_message: str = "", error_detail: bytes = b"") -> None:
        self.code = code
        self.public_message = public_message
        self.error_detail = error_detail
        super().__init__(f"call failed: {status_name(code)} {public_message}".rstrip())


# 稳定的 StatusCode 名字表，仅用于诊断/日志（不参与程序判断）。
_STATUS_NAMES = {
    status.STATUS_CODE_UNSPECIFIED: "UNSPECIFIED",
    status.STATUS_CODE_OK: "OK",
    status.STATUS_CODE_ACCEPTED: "ACCEPTED",
    status.STATUS_CODE_INVALID_ARGUMENT: "INVALID_ARGUMENT",
    status.STATUS_CODE_UNAUTHENTICATED: "UNAUTHENTICATED",
    status.STATUS_CODE_PERMISSION_DENIED: "PERMISSION_DENIED",
    status.STATUS_CODE_NOT_FOUND: "NOT_FOUND",
    status.STATUS_CODE_FAILED_PRECONDITION: "FAILED_PRECONDITION",
    status.STATUS_CODE_RESOURCE_EXHAUSTED: "RESOURCE_EXHAUSTED",
    status.STATUS_CODE_DEADLINE_EXCEEDED: "DEADLINE_EXCEEDED",
    status.STATUS_CODE_CANCELLED: "CANCELLED",
    status.STATUS_CODE_UNAVAILABLE: "UNAVAILABLE",
    status.STATUS_CODE_INTERNAL: "INTERNAL",
}


def status_name(code: int) -> str:
    return _STATUS_NAMES.get(code, f"STATUS_CODE({int(code)})")


def is_success_code(code: int) -> bool:
    return code in (status.STATUS_CODE_OK, status.STATUS_CODE_ACCEPTED)


def validate_success_code(code: int) -> bool:
    """Success outcome 不变量：code ∈ {OK, ACCEPTED}（status.proto）。"""
    return is_success_code(code)


def validate_failure_code(code: int) -> bool:
    """Failure outcome 不变量：code ∉ {UNSPECIFIED, OK, ACCEPTED}（status.proto）。"""
    return code not in (
        status.STATUS_CODE_UNSPECIFIED,
        status.STATUS_CODE_OK,
        status.STATUS_CODE_ACCEPTED,
    )


def typed_reason(error_detail: bytes, detail_cls: type[Message]) -> Optional[int]:
    """把 `error_detail` bytes 按给定 detail 类型解出 `reason`（位置 B）。

    - detail 为空 → None（无细因）。
    - detail 解不开 → None（**不抛异常、不判协议损坏**）：畸形 detail 由 nervud
      归一化为 INTERNAL，调用者应回退到通用 code（golden vector
      `failure_malformed_detail_internal`）。
    - detail 解得开 → 返回 `reason` 的整数值；**未知枚举值（如 99）原样返回**，
      调用者保留外层通用 code 不变（golden vector `*_unknown_reason`）。

    返回 int 而非枚举，正是为了让未知 reason 也能无损透出，不被枚举校验吞掉。
    """
    if not error_detail:
        return None
    msg = detail_cls()
    try:
        msg.ParseFromString(error_detail)
    except Exception:  # noqa: BLE001 —— 畸形 detail 不是协议损坏，回退通用 code
        return None
    if not msg.DESCRIPTOR.fields_by_name.get("reason"):
        return None
    return int(msg.reason)
