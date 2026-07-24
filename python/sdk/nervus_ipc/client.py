"""Nervus IPC v1 —— 消费侧 client（供 Agent / 服务调用标准接口）。

覆盖 C 组 client 面：连 UDS / Hello / ResolveEndpoint / 调用 / AcquireControl / 订阅 /
错误解码。行为与 Kotlin `NervusClient` 对齐（reader 线程 + pending future + ping 保活 +
断线时全部 pending 完结为 Disconnected）。

同步 API：`call/resolve_endpoint/acquire_control/...` 阻塞到终结响应或超时；底层由后台
reader 线程 + `concurrent.futures.Future` 驱动。
"""

from __future__ import annotations

import logging
import socket
import threading
from concurrent.futures import TimeoutError as FutureTimeoutError
from dataclasses import dataclass
from typing import Dict, Optional, Set

from nervus.ipc.v1 import envelope_pb2 as ipc

from . import errors
from .errors import CallFailed, Disconnected, NervusError, ProtocolViolation, Timeout
from .handshake import HandshakeResult, negotiate
from .rpc import PendingMap, RequestIdGenerator
from .subscription import Subscription
from .wire import FrameReader, FrameWriter, connect_unix

_log = logging.getLogger("nervus_ipc.client")


@dataclass(frozen=True)
class ResolvedEndpoint:
    """ResolveEndpoint 成功结果（§10.5）。endpoint_id 是连接作用域句柄，断线即失效。"""

    endpoint_id: int
    interface_major: int
    interface_minor: int
    interface_schema_hash: bytes
    resource_handle: str


@dataclass(frozen=True)
class ControlLease:
    """AcquireControl 成功结果（§10.2、A5）。lease 绑本连接、不可转让。"""

    lease_id: int
    motion_epoch: int
    deadline_nanos: int
    resource_handle: str


def _ms_to_wait_seconds(timeout_ms: Optional[int]) -> Optional[float]:
    if timeout_ms is None or timeout_ms <= 0:
        return None
    # 本地等待留一点余量，让服务端的 DEADLINE_EXCEEDED 优先于本地超时抛出。
    return timeout_ms / 1000.0 + 1.0


class NervusClient:
    def __init__(
        self,
        sdk_name: str = "nervus-python-sdk",
        sdk_version: str = "0.1.0",
        *,
        min_protocol_major: int = 1,
        max_protocol_major: int = 1,
        max_protocol_minor: int = 0,
    ) -> None:
        self.sdk_name = sdk_name
        self.sdk_version = sdk_version
        self._min_major = min_protocol_major
        self._max_major = max_protocol_major
        self._max_minor = max_protocol_minor

        self._sock: Optional[socket.socket] = None
        self._reader: Optional[FrameReader] = None
        self._writer: Optional[FrameWriter] = None
        self._handshake: Optional[HandshakeResult] = None

        self._ids = RequestIdGenerator()
        self._pending = PendingMap()

        self._subs_lock = threading.Lock()
        self._subs_by_id: Dict[int, Subscription] = {}
        self._subs_by_endpoint: Dict[int, Set[int]] = {}

        self._reader_thread: Optional[threading.Thread] = None
        self._ping_thread: Optional[threading.Thread] = None
        self._running = threading.Event()

    # ---- properties ------------------------------------------------------
    @property
    def limits(self) -> Optional[ipc.ConnectionLimits]:
        return self._handshake.limits if self._handshake else None

    @property
    def connected(self) -> bool:
        return self._running.is_set()

    # ---- lifecycle -------------------------------------------------------
    def connect(self, socket_path: str, component_id: str = "") -> HandshakeResult:
        if self._running.is_set():
            raise NervusError("already connected")
        sock = connect_unix(socket_path)
        try:
            reader = FrameReader(sock)
            writer = FrameWriter(sock)
            result = negotiate(
                reader,
                writer,
                min_major=self._min_major,
                max_major=self._max_major,
                max_minor=self._max_minor,
                sdk_name=self.sdk_name,
                sdk_version=self.sdk_version,
                component_id=component_id,
            )
        except BaseException:
            sock.close()
            raise

        self._sock = sock
        self._reader = reader
        self._writer = writer
        self._handshake = result
        self._running.set()

        # reader 线程之外用一个较短的 socket 超时，让循环能周期性检查 _running。
        idle_ms = result.limits.idle_timeout_ms
        sock.settimeout(min(idle_ms / 1000.0, 1.0) if idle_ms > 0 else 1.0)

        self._reader_thread = threading.Thread(
            target=self._reader_loop, name="nervus-client-reader", daemon=True
        )
        self._reader_thread.start()
        if idle_ms > 0:
            self._ping_thread = threading.Thread(
                target=self._ping_loop, args=(idle_ms / 1000.0 / 2.0,), name="nervus-client-ping", daemon=True
            )
            self._ping_thread.start()
        return result

    def close(self) -> None:
        self._teardown(Disconnected("client closed"))

    def __enter__(self) -> "NervusClient":
        return self

    def __exit__(self, *exc) -> None:
        self.close()

    def _teardown(self, exc: BaseException) -> None:
        if not self._running.is_set():
            return
        self._running.clear()
        try:
            if self._sock is not None:
                self._sock.close()
        except OSError:
            pass
        self._pending.fail_all(lambda: exc)
        with self._subs_lock:
            subs = list(self._subs_by_id.values())
            self._subs_by_id.clear()
            self._subs_by_endpoint.clear()
        for sub in subs:
            sub._close()

    def _write(self, env: ipc.Envelope) -> None:
        writer = self._writer
        if writer is None or not self._running.is_set():
            raise NervusError("not connected")
        writer.write_frame(env)

    # ---- reader loop -----------------------------------------------------
    def _reader_loop(self) -> None:
        try:
            while self._running.is_set():
                try:
                    env = self._reader.read_frame()  # type: ignore[union-attr]
                except socket.timeout:
                    continue
                except (Disconnected, ProtocolViolation) as e:
                    if isinstance(e, ProtocolViolation):
                        _log.error("protocol violation: %s", e)
                    break
                self._dispatch(env)
        except Exception as e:  # noqa: BLE001
            _log.debug("reader loop exited: %s", e)
        finally:
            self._teardown(Disconnected("connection closed by server"))

    def _dispatch(self, env: ipc.Envelope) -> None:
        body = env.WhichOneof("body")
        if body == "response":
            self._on_response(env.response)
        elif body == "resolve_endpoint_result":
            self._pending.complete(env.resolve_endpoint_result.request_id, env.resolve_endpoint_result)
        elif body == "acquire_control_result":
            self._pending.complete(env.acquire_control_result.request_id, env.acquire_control_result)
        elif body == "release_control_result":
            self._pending.complete(env.release_control_result.request_id, env.release_control_result)
        elif body == "subscribe_result":
            self._on_subscribe_result(env.subscribe_result)
        elif body == "unsubscribe_result":
            self._pending.complete(env.unsubscribe_result.request_id, env.unsubscribe_result)
        elif body == "event":
            self._on_event(env.event)
        elif body == "endpoint_died":
            self._on_endpoint_gone(env.endpoint_died.endpoint_id, f"died({env.endpoint_died.reason})", retry=True)
        elif body == "endpoint_revoked":
            self._on_endpoint_gone(env.endpoint_revoked.endpoint_id, f"revoked({env.endpoint_revoked.reason})", retry=False)
        elif body == "subscription_closed":
            self._on_subscription_closed(env.subscription_closed)
        elif body == "ping":
            self._safe_write(ipc.Envelope(pong=ipc.Pong(nonce=env.ping.nonce)))
        elif body == "pong":
            pass
        else:
            # 未知/未设置 body 一律协议违规：关闭连接，不生成响应（§10.4）。
            _log.error("unknown envelope body %r - closing connection", body)
            self._teardown(ProtocolViolation(f"unknown envelope body {body!r}"))

    def _safe_write(self, env: ipc.Envelope) -> None:
        try:
            self._write(env)
        except Exception as e:  # noqa: BLE001
            _log.debug("write failed: %s", e)

    def _on_response(self, resp: ipc.Response) -> None:
        outcome = resp.WhichOneof("outcome")
        if outcome == "success":
            if not errors.validate_success_code(resp.success.code):
                _log.error("invalid success code %s - dropping", resp.success.code)
                return
        elif outcome == "failure":
            if not errors.validate_failure_code(resp.failure.code):
                _log.error("invalid failure code %s - dropping", resp.failure.code)
                return
        else:
            _log.warning("response with no outcome - dropping")
            return
        self._pending.complete(resp.request_id, resp)

    def _on_subscribe_result(self, result: ipc.SubscribeResult) -> None:
        # 绑定发生在 reader 线程内，先于后续任何 Event —— 避免早到事件丢失。
        outcome = result.WhichOneof("outcome")
        if outcome == "success":
            s = result.success
            sub = Subscription(
                subscription_id=s.subscription_id,
                delivery_class=s.delivery_class,
            )
            with self._subs_lock:
                self._subs_by_id[s.subscription_id] = sub
            self._pending.complete(result.request_id, ("ok", sub))
        elif outcome == "failure":
            self._pending.complete(result.request_id, ("err", result.failure))
        else:
            self._pending.fail_one(result.request_id, ProtocolViolation("subscribe_result with no outcome"))

    def _on_event(self, event: ipc.Event) -> None:
        with self._subs_lock:
            sub = self._subs_by_id.get(event.subscription_id)
        if sub is not None:
            sub._push(event)

    def _on_subscription_closed(self, closed: ipc.SubscriptionClosed) -> None:
        with self._subs_lock:
            sub = self._subs_by_id.pop(closed.subscription_id, None)
            for ids in self._subs_by_endpoint.values():
                ids.discard(closed.subscription_id)
        if sub is not None:
            sub._close(reason=closed.reason)

    def _on_endpoint_gone(self, endpoint_id: int, why: str, *, retry: bool) -> None:
        level = logging.INFO if retry else logging.WARNING
        _log.log(level, "endpoint %d %s (retry=%s)", endpoint_id, why, retry)
        with self._subs_lock:
            sub_ids = self._subs_by_endpoint.pop(endpoint_id, set())
            subs = [self._subs_by_id.pop(sid) for sid in sub_ids if sid in self._subs_by_id]
        for sub in subs:
            sub._close()

    def _ping_loop(self, interval: float) -> None:
        import time

        while self._running.is_set():
            deadline = time.monotonic() + interval
            while self._running.is_set() and time.monotonic() < deadline:
                time.sleep(min(0.1, max(0.0, deadline - time.monotonic())))
            if not self._running.is_set():
                break
            self._safe_write(ipc.Envelope(ping=ipc.Ping(nonce=self._ids.next())))

    # ---- public API ------------------------------------------------------
    def _default_timeout_ms(self, timeout_ms: Optional[int]) -> int:
        if timeout_ms is not None:
            return timeout_ms
        lim = self.limits
        return lim.default_timeout_ms if lim and lim.default_timeout_ms > 0 else 5000

    def _await(self, request_id: int, fut, timeout_ms: int):
        try:
            return fut.result(timeout=_ms_to_wait_seconds(timeout_ms))
        except FutureTimeoutError as e:
            self._pending.discard(request_id)
            raise Timeout(f"request {request_id} timed out after {timeout_ms}ms") from e

    def resolve_endpoint(
        self,
        interface_id: str,
        *,
        min_interface_major: int = 1,
        max_interface_major: int = 1,
        resource_type: str = "",
        resource_role: str = "",
        explicit_component: str = "",
        timeout_ms: Optional[int] = None,
    ) -> ResolvedEndpoint:
        """解析逻辑接口 → 连接作用域 endpoint 句柄。失败抛 :class:`CallFailed`（带 typed reason）。"""
        timeout_ms = self._default_timeout_ms(timeout_ms)
        rid = self._ids.next()
        fut = self._pending.register(rid)
        req = ipc.ResolveEndpoint(
            request_id=rid,
            interface_id=interface_id,
            min_interface_major=min_interface_major,
            max_interface_major=max_interface_major,
        )
        if resource_type or resource_role:
            req.selector.type = resource_type
            req.selector.role = resource_role
        if explicit_component:
            req.explicit_component = explicit_component
        self._write_or_fail(rid, ipc.Envelope(resolve_endpoint=req))
        result: ipc.ResolveEndpointResult = self._await(rid, fut, timeout_ms)
        if result.WhichOneof("outcome") == "success":
            s = result.success
            return ResolvedEndpoint(
                endpoint_id=s.endpoint_id,
                interface_major=s.interface_major,
                interface_minor=s.interface_minor,
                interface_schema_hash=bytes(s.interface_schema_hash),
                resource_handle=s.resource_handle,
            )
        f = result.failure
        raise CallFailed(f.code, f.public_message, bytes(f.error_detail))

    def call(
        self,
        endpoint_id: int,
        method_id: int,
        payload: bytes = b"",
        *,
        timeout_ms: Optional[int] = None,
    ) -> ipc.Response:
        """调用一个方法，返回原始 Response（Success/Failure outcome）。

        typed detail 由调用方按方法 schema 用 :func:`errors.typed_reason` 解码——
        因为 detail 类型由 endpoint + interface_version + method_id 唯一决定，
        SDK 不猜（status.proto：不接受发送方自报类型）。
        """
        timeout_ms = self._default_timeout_ms(timeout_ms)
        lim = self.limits
        if lim is not None:
            if lim.default_method_payload_bytes > 0 and len(payload) > lim.default_method_payload_bytes:
                raise NervusError(f"payload too large: {len(payload)} > {lim.default_method_payload_bytes}")
            if lim.max_inflight_requests > 0 and self._pending.size() >= lim.max_inflight_requests:
                raise NervusError(f"max inflight requests ({lim.max_inflight_requests}) reached")
            if lim.max_timeout_ms > 0:
                timeout_ms = min(timeout_ms, lim.max_timeout_ms)
        rid = self._ids.next()
        fut = self._pending.register(rid)
        req = ipc.Request(
            request_id=rid,
            endpoint_id=endpoint_id,
            method_id=method_id,
            timeout_ms=timeout_ms,
            payload=payload,
        )
        self._write_or_fail(rid, ipc.Envelope(request=req))
        return self._await(rid, fut, timeout_ms)

    def acquire_control(
        self,
        controller_class: int,
        *,
        resource_type: str,
        resource_role: str,
        requested_deadline_nanos: int = 0,
        timeout_ms: Optional[int] = None,
    ) -> ControlLease:
        """申请控制租约（HUMAN/AI）。失败抛 :class:`CallFailed`（typed ControlLeaseErrorReason）。

        客户端**不能**申请 NONE（ControllerClass 无 NONE）；可信字段由 nervud 派发时
        附加，App 不在任何 Request payload 里自填（§10.2/§10.3）。
        """
        timeout_ms = self._default_timeout_ms(timeout_ms)
        rid = self._ids.next()
        fut = self._pending.register(rid)
        acq = ipc.AcquireControl(
            request_id=rid,
            controller_class=controller_class,
            resource=ipc.ResourceSelector(type=resource_type, role=resource_role),
            requested_deadline_nanos=requested_deadline_nanos,
        )
        self._write_or_fail(rid, ipc.Envelope(acquire_control=acq))
        result: ipc.AcquireControlResult = self._await(rid, fut, timeout_ms)
        if result.WhichOneof("outcome") == "success":
            s = result.success
            return ControlLease(
                lease_id=s.lease_id,
                motion_epoch=s.motion_epoch,
                deadline_nanos=s.deadline_nanos,
                resource_handle=s.resource_handle,
            )
        f = result.failure
        raise CallFailed(f.code, f.public_message, bytes(f.error_detail))

    def release_control(self, lease_id: int, *, timeout_ms: Optional[int] = None) -> None:
        timeout_ms = self._default_timeout_ms(timeout_ms)
        rid = self._ids.next()
        fut = self._pending.register(rid)
        rel = ipc.ReleaseControl(request_id=rid, lease_id=lease_id)
        self._write_or_fail(rid, ipc.Envelope(release_control=rel))
        result: ipc.ReleaseControlResult = self._await(rid, fut, timeout_ms)
        if result.WhichOneof("outcome") == "failure":
            f = result.failure
            raise CallFailed(f.code, f.public_message, bytes(f.error_detail))

    def subscribe(
        self,
        endpoint_id: int,
        event_id: int,
        payload: bytes = b"",
        *,
        timeout_ms: Optional[int] = None,
    ) -> Subscription:
        """订阅 (endpoint, event_id) 上的事件，返回可迭代的 :class:`Subscription`。"""
        timeout_ms = self._default_timeout_ms(timeout_ms)
        lim = self.limits
        if lim is not None and lim.max_subscriptions > 0:
            with self._subs_lock:
                if len(self._subs_by_id) >= lim.max_subscriptions:
                    raise NervusError(f"max subscriptions ({lim.max_subscriptions}) reached")
        rid = self._ids.next()
        fut = self._pending.register(rid)
        sub_msg = ipc.Subscribe(request_id=rid, endpoint_id=endpoint_id, event_id=event_id, payload=payload)
        self._write_or_fail(rid, ipc.Envelope(subscribe=sub_msg))
        kind, value = self._await(rid, fut, timeout_ms)
        if kind == "err":
            f = value
            raise CallFailed(f.code, f.public_message, bytes(f.error_detail))
        sub: Subscription = value
        sub._unsubscribe_fn = lambda: self.unsubscribe(sub.subscription_id)
        with self._subs_lock:
            self._subs_by_endpoint.setdefault(endpoint_id, set()).add(sub.subscription_id)
        return sub

    def unsubscribe(self, subscription_id: int, *, timeout_ms: Optional[int] = None) -> None:
        timeout_ms = self._default_timeout_ms(timeout_ms)
        rid = self._ids.next()
        fut = self._pending.register(rid)
        self._write_or_fail(rid, ipc.Envelope(unsubscribe=ipc.Unsubscribe(request_id=rid, subscription_id=subscription_id)))
        result: ipc.UnsubscribeResult = self._await(rid, fut, timeout_ms)
        with self._subs_lock:
            sub = self._subs_by_id.pop(subscription_id, None)
            for ids in self._subs_by_endpoint.values():
                ids.discard(subscription_id)
        if sub is not None:
            sub._close()
        if result.WhichOneof("outcome") == "failure":
            f = result.failure
            raise CallFailed(f.code, f.public_message, bytes(f.error_detail))

    def cancel(self, request_id: int) -> None:
        """尽力取消（§10.7）：不保证停止已提交的工作，被取消请求的终结 Response 即结果。"""
        self._pending.discard(request_id)
        self._safe_write(ipc.Envelope(cancel=ipc.Cancel(request_id=request_id)))

    def _write_or_fail(self, request_id: int, env: ipc.Envelope) -> None:
        try:
            self._write(env)
        except Exception as e:  # noqa: BLE001
            self._pending.fail_one(request_id, Disconnected(f"write failed: {e}"))
