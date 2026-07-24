"""client 冒烟：resolve → call（+ acquire/release/subscribe），走真 AF_UNIX。"""

from __future__ import annotations

import pytest

from nervus.ipc.v1 import envelope_pb2 as ipc
from nervus.ipc.v1 import status_pb2 as status
from nervus_ipc import CallFailed, NervusClient
from _fakenervud import FakeNervud


def _responder(conn, env):
    body = env.WhichOneof("body")
    if body == "resolve_endpoint":
        r = env.resolve_endpoint
        conn.send(
            ipc.Envelope(
                resolve_endpoint_result=ipc.ResolveEndpointResult(
                    request_id=r.request_id,
                    success=ipc.ResolveEndpointSuccess(
                        endpoint_id=42,
                        interface_major=1,
                        interface_minor=0,
                        resource_handle="base.main",
                    ),
                )
            )
        )
    elif body == "request":
        rq = env.request
        # echo：把 payload 原样放进 success
        conn.send(
            ipc.Envelope(
                response=ipc.Response(
                    request_id=rq.request_id,
                    success=status.Success(code=status.STATUS_CODE_OK, payload=rq.payload),
                )
            )
        )
    elif body == "acquire_control":
        a = env.acquire_control
        conn.send(
            ipc.Envelope(
                acquire_control_result=ipc.AcquireControlResult(
                    request_id=a.request_id,
                    success=ipc.AcquireControlSuccess(
                        lease_id=0xABCD, motion_epoch=1, deadline_nanos=500_000_000, resource_handle="base.main"
                    ),
                )
            )
        )
    elif body == "release_control":
        rc = env.release_control
        conn.send(
            ipc.Envelope(
                release_control_result=ipc.ReleaseControlResult(
                    request_id=rc.request_id, success=status.Success(code=status.STATUS_CODE_OK)
                )
            )
        )
    elif body == "subscribe":
        s = env.subscribe
        conn.send(
            ipc.Envelope(
                subscribe_result=ipc.SubscribeResult(
                    request_id=s.request_id,
                    success=ipc.SubscribeSuccess(subscription_id=7, delivery_class=ipc.DELIVERY_CLASS_STATE),
                )
            )
        )
        # 推一条事件
        conn.send(
            ipc.Envelope(
                event=ipc.Event(subscription_id=7, sequence=1, endpoint_id=42, event_id=1, payload=b"tick")
            )
        )
    elif body == "unsubscribe":
        u = env.unsubscribe
        conn.send(
            ipc.Envelope(
                unsubscribe_result=ipc.UnsubscribeResult(
                    request_id=u.request_id, success=ipc.UnsubscribeSuccess()
                )
            )
        )


def test_resolve_and_call_echo():
    with FakeNervud(on_frame=_responder) as nv:
        client = NervusClient()
        client.connect(nv.path, component_id="test.comp")
        try:
            ep = client.resolve_endpoint("nervus.interface.motion.base", resource_type="nervus.resource.motion.base", resource_role="main")
            assert ep.endpoint_id == 42
            assert ep.resource_handle == "base.main"

            resp = client.call(ep.endpoint_id, method_id=1, payload=b"hello-nervus")
            assert resp.WhichOneof("outcome") == "success"
            assert resp.success.code == status.STATUS_CODE_OK
            assert resp.success.payload == b"hello-nervus"
        finally:
            client.close()


def test_acquire_and_release_control():
    with FakeNervud(on_frame=_responder) as nv:
        with NervusClient() as client:
            client.connect(nv.path)
            lease = client.acquire_control(
                ipc.CONTROLLER_CLASS_HUMAN,
                resource_type="nervus.resource.motion.base",
                resource_role="main",
                requested_deadline_nanos=500_000_000,
            )
            assert lease.lease_id == 0xABCD
            assert lease.motion_epoch == 1
            client.release_control(lease.lease_id)


def test_subscribe_receives_event():
    with FakeNervud(on_frame=_responder) as nv:
        with NervusClient() as client:
            client.connect(nv.path)
            sub = client.subscribe(endpoint_id=42, event_id=1)
            assert sub.delivery_class == ipc.DELIVERY_CLASS_STATE
            ev = sub.next_event(timeout=5.0)
            assert ev is not None
            assert ev.payload == b"tick"
            assert ev.sequence == 1


def test_acquire_control_failure_typed_reason():
    def deny(conn, env):
        if env.WhichOneof("body") == "acquire_control":
            a = env.acquire_control
            d = ipc.ControlLeaseErrorDetail(reason=ipc.CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN)
            conn.send(
                ipc.Envelope(
                    acquire_control_result=ipc.AcquireControlResult(
                        request_id=a.request_id,
                        failure=status.Failure(
                            code=status.STATUS_CODE_FAILED_PRECONDITION, error_detail=d.SerializeToString()
                        ),
                    )
                )
            )

    with FakeNervud(on_frame=deny) as nv:
        with NervusClient() as client:
            client.connect(nv.path)
            with pytest.raises(CallFailed) as ei:
                client.acquire_control(
                    ipc.CONTROLLER_CLASS_AI, resource_type="nervus.resource.motion.base", resource_role="main"
                )
            assert ei.value.code == status.STATUS_CODE_FAILED_PRECONDITION
            from nervus_ipc import typed_reason

            assert typed_reason(ei.value.error_detail, ipc.ControlLeaseErrorDetail) == ipc.CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN
