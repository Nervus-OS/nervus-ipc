"""ServiceHost 冒烟：register → 收 Dispatch（带 ExecutionContext）→ 回 DispatchResult，
外加 fail-closed 复核（未知 method / 过 deadline / Safety 锁存 / 缺身份的控制调用）。"""

from __future__ import annotations

from nervus.ipc.v1 import envelope_pb2 as ipc
from nervus.ipc.v1 import status_pb2 as status
from nervus_ipc import DispatchOutcome, NervusServiceHost, SafetyState
from _fakenervud import FakeNervud


ECHO_METHOD = 1
MOTION_METHOD = 2
CONTROL_METHOD = 3


def _register_responder(endpoint_id: int):
    def on_frame(conn, env):
        if env.WhichOneof("body") == "register_endpoint":
            r = env.register_endpoint
            conn.send(
                ipc.Envelope(
                    register_endpoint_result=ipc.RegisterEndpointResult(
                        request_id=r.request_id,
                        success=ipc.RegisterEndpointSuccess(endpoint_id=endpoint_id),
                    )
                )
            )

    return on_frame


def _connect_host(nv, **host_kwargs):
    host = NervusServiceHost(**host_kwargs)
    host.connect(nv.path, component_id="provider.comp")
    return host


def _dispatch(endpoint_id, method_id, payload=b"", *, route_id=1, remaining_ms=5000, caller_pkg="caller.pkg"):
    return ipc.Envelope(
        dispatch=ipc.Dispatch(
            route_id=route_id,
            endpoint_id=endpoint_id,
            method_id=method_id,
            remaining_ms=remaining_ms,
            payload=payload,
            caller=ipc.CallerContext(package_id=caller_pkg, component_id="caller.comp", uid=1000, gid=1000, pid=4242),
        )
    )


def _recv_dispatch_result(nv):
    while True:
        env = nv.recv(timeout=5.0)
        if env.WhichOneof("body") == "dispatch_result":
            return env.dispatch_result


def test_register_dispatch_echo():
    with FakeNervud(on_frame=_register_responder(100)) as nv:
        host = _connect_host(nv)
        try:
            def echo(payload, ctx):
                # 身份只来自 ctx.caller（nervud 附加），不从 payload 读
                assert ctx.package_id == "caller.pkg"
                assert ctx.endpoint_id == 100
                return DispatchOutcome.success(payload=payload)

            host.register_method(ECHO_METHOD, echo)
            reg = host.register_endpoint("nervus.interface.motion.base", 1, 0, resource_handle="base.main")
            assert reg.endpoint_id == 100

            nv.send(_dispatch(100, ECHO_METHOD, payload=b"ping-echo"))
            result = _recv_dispatch_result(nv)
            assert result.route_id == 1
            assert result.WhichOneof("outcome") == "success"
            assert result.success.code == status.STATUS_CODE_OK
            assert result.success.payload == b"ping-echo"
        finally:
            host.close()


def test_unknown_method_not_found():
    with FakeNervud(on_frame=_register_responder(100)) as nv:
        host = _connect_host(nv)
        try:
            host.register_endpoint("nervus.interface.motion.base", 1, 0)
            nv.send(_dispatch(100, method_id=999, route_id=2))
            result = _recv_dispatch_result(nv)
            assert result.WhichOneof("outcome") == "failure"
            assert result.failure.code == status.STATUS_CODE_NOT_FOUND
        finally:
            host.close()


def test_deadline_exceeded_rejected_before_handler():
    with FakeNervud(on_frame=_register_responder(100)) as nv:
        host = _connect_host(nv)
        called = {"n": 0}
        try:
            def slow(payload, ctx):
                called["n"] += 1
                return DispatchOutcome.success()

            host.register_method(ECHO_METHOD, slow)
            host.register_endpoint("nervus.interface.motion.base", 1, 0)
            # remaining_ms=0 → 过 deadline，handler 不应被调用
            nv.send(_dispatch(100, ECHO_METHOD, route_id=3, remaining_ms=0))
            result = _recv_dispatch_result(nv)
            assert result.failure.code == status.STATUS_CODE_DEADLINE_EXCEEDED
            assert called["n"] == 0
        finally:
            host.close()


def test_safety_latched_blocks_motion():
    safety_state = SafetyState()
    with FakeNervud(on_frame=_register_responder(100)) as nv:
        host = _connect_host(nv, safety_state=safety_state)
        try:
            def motion(payload, ctx):
                return DispatchOutcome.success()

            host.register_method(MOTION_METHOD, motion, is_motion=True)
            host.register_endpoint("nervus.interface.motion.base", 1, 0)

            # 未锁存：正常通过
            nv.send(_dispatch(100, MOTION_METHOD, route_id=4))
            assert _recv_dispatch_result(nv).WhichOneof("outcome") == "success"

            # 收到 SafetyHalt（模拟）→ 锁存 → 普通运动调用被拒
            safety_state.latch(motion_epoch=5)
            nv.send(_dispatch(100, MOTION_METHOD, route_id=5))
            result = _recv_dispatch_result(nv)
            assert result.failure.code == status.STATUS_CODE_FAILED_PRECONDITION
            assert "safety" in result.failure.public_message.lower()

            # 受控 re-arm 后恢复
            safety_state.rearm()
            nv.send(_dispatch(100, MOTION_METHOD, route_id=6))
            assert _recv_dispatch_result(nv).WhichOneof("outcome") == "success"
        finally:
            host.close()


def test_control_call_without_identity_rejected():
    with FakeNervud(on_frame=_register_responder(100)) as nv:
        host = _connect_host(nv)
        try:
            host.register_method(CONTROL_METHOD, lambda p, c: DispatchOutcome.success(), requires_control_lease=True)
            host.register_endpoint("nervus.interface.motion.base", 1, 0)
            # caller_pkg 为空 → 缺 ExecutionContext 身份 → 控制调用被拒（fail-closed）
            nv.send(_dispatch(100, CONTROL_METHOD, route_id=7, caller_pkg=""))
            result = _recv_dispatch_result(nv)
            assert result.failure.code == status.STATUS_CODE_FAILED_PRECONDITION
        finally:
            host.close()


def test_cancel_dispatch_then_dispatch_is_cancelled():
    with FakeNervud(on_frame=_register_responder(100)) as nv:
        host = _connect_host(nv)
        try:
            host.register_method(ECHO_METHOD, lambda p, c: DispatchOutcome.success(payload=p))
            host.register_endpoint("nervus.interface.motion.base", 1, 0)
            # 先取消 route 8，再发同 route 的 dispatch → CANCELLED
            nv.send(ipc.Envelope(cancel_dispatch=ipc.CancelDispatch(route_id=8, reason=ipc.CANCEL_DISPATCH_REASON_CLIENT_CANCELLED)))
            import time

            time.sleep(0.2)
            nv.send(_dispatch(100, ECHO_METHOD, route_id=8))
            result = _recv_dispatch_result(nv)
            assert result.failure.code == status.STATUS_CODE_CANCELLED
        finally:
            host.close()
