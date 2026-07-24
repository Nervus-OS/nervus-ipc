"""Hello / HelloAck 握手（§10.2）。

Hello 是连接上的第一个 Envelope。此前 nervud 已通过 SO_PEERCRED 拿到 PID/UID/GID
并映射出 Package——所以 Hello 里没有任何身份字段可被相信；`declared_component_id`
只是待验证线索。握手前不接受任何其他 body，握手有独立短 deadline。

失败时 nervud 先发带 Failure 的 HelloAck 再关连接，让客户端能区分「版本谈不拢」
（不该重连）与「socket 坏了」（该重连）——所以裸 EOF 与 Failure HelloAck 分别处理。
"""

from __future__ import annotations

from dataclasses import dataclass

from nervus.ipc.v1 import envelope_pb2 as ipc

from .errors import HandshakeRejected, ProtocolViolation
from .wire import FrameReader, FrameWriter


@dataclass(frozen=True)
class HandshakeResult:
    protocol_major: int
    protocol_minor: int
    limits: ipc.ConnectionLimits
    package_id: str
    component_id: str


def negotiate(
    reader: FrameReader,
    writer: FrameWriter,
    *,
    min_major: int,
    max_major: int,
    max_minor: int,
    sdk_name: str,
    sdk_version: str,
    component_id: str = "",
) -> HandshakeResult:
    hello = ipc.Hello(
        min_protocol_major=min_major,
        max_protocol_major=max_major,
        max_protocol_minor=max_minor,
        sdk_name=sdk_name,
        sdk_version=sdk_version,
        declared_component_id=component_id,
    )
    writer.write_frame(ipc.Envelope(hello=hello))

    env = reader.read_frame()
    if env.WhichOneof("body") != "hello_ack":
        raise ProtocolViolation(f"expected HelloAck, got {env.WhichOneof('body')}")

    ack = env.hello_ack
    outcome = ack.WhichOneof("outcome")
    if outcome == "success":
        s = ack.success
        chosen = s.protocol_major
        if chosen < min_major or chosen > max_major:
            raise ProtocolViolation(
                f"server chose incompatible protocol major {chosen}, "
                f"client supports [{min_major}, {max_major}]"
            )
        return HandshakeResult(
            protocol_major=chosen,
            protocol_minor=s.protocol_minor,
            limits=s.limits,
            package_id=s.package_id,
            component_id=s.component_id,
        )
    if outcome == "failure":
        raise HandshakeRejected(ack.failure.code, ack.failure.public_message)
    raise ProtocolViolation("HelloAck with no outcome")
