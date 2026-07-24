"""Nervus IPC v1 —— 提供侧 ServiceHost（供 ROS provider 注册实现）。

覆盖 C 组 ServiceHost 面：RegisterEndpoint / 收 Dispatch（带 nervud 生成的
ExecutionContext）/ 回 DispatchResult / operation 上报 / 高优先级 Safety Path。
行为与 Kotlin `NervusServiceHost` 对齐（reader 线程、未知 method → NOT_FOUND、
CancelDispatch → CANCELLED、退出时优雅 Unregister）。

fail-closed（NRCP §10.4，本 SDK 只复核不裁决）：
  - 缺 ExecutionContext 的控制调用拒绝；
  - 旧 motion_epoch 拒绝（陈旧世代）；
  - 过 deadline 拒绝；
  - 收 SafetyHalt 后不接普通运动调用（本地 SafetyState 镜像）；
  - **不信 payload 里的 `source=HUMAN` 等字符串**——身份只认 nervud 附加的
    CallerContext（ctx.caller），handler 拿不到、也不该从 payload 里读身份。

身份由 nervud 侧经 SO_PEERCRED 核验，Python 侧不自报（同 client）。
"""

from __future__ import annotations

import logging
import socket
import threading
import time
from dataclasses import dataclass
from typing import Callable, Dict, Optional

from nervus.ipc.v1 import envelope_pb2 as ipc
from nervus.ipc.v1 import status_pb2 as status

from .errors import CallFailed, Disconnected, NervusError, ProtocolViolation
from .handshake import HandshakeResult, negotiate
from .operation import OperationReporter, OperationTransport
from .rpc import PendingMap, RequestIdGenerator
from .safety import SafetyState
from .wire import FrameReader, FrameWriter, connect_unix

_log = logging.getLogger("nervus_ipc.service_host")


@dataclass(frozen=True)
class Registration:
    endpoint_id: int
    interface_id: str
    interface_major: int
    interface_minor: int


@dataclass
class ExecutionContext:
    """nervud 在派发给 Provider 时附加的可信上下文（§10.2/§10.3）。

    当前从冻结的 Dispatch wire 取到的可信字段：`caller`（CallerContext）+ `route_id`
    + `remaining_ms`（→ 单调 deadline）。lease_id/motion_epoch/operation_id 是 nervud
    B1 dispatch 落地后才会附到 Dispatch 上的字段（NRCP §10.3 的 ExecutionContext 全集），
    本 SDK 先留字段、默认缺省（0），fail-closed 复核用「已在 wire 上的」那部分执行，
    lease/epoch/operation 相关校验在 B1 补齐字段后即刻生效——接口面不变。

    **可信字段的唯一来源是本对象**（由 nervud 生成）；handler 绝不从 payload 里读
    `source=HUMAN`/包名之类的自报身份。
    """

    route_id: int
    endpoint_id: int
    method_id: int
    remaining_ms: int
    caller: ipc.CallerContext
    _deadline_monotonic: float
    # --- B1 dispatch 落地后由 Dispatch 附加（当前 wire 未携带，缺省 0）---
    lease_id: int = 0
    motion_epoch: int = 0
    operation_id: int = 0

    @property
    def package_id(self) -> str:
        return self.caller.package_id

    @property
    def component_id(self) -> str:
        return self.caller.component_id

    @property
    def granted_permissions(self):
        return list(self.caller.granted_permissions)

    def has_caller_identity(self) -> bool:
        return bool(self.caller.package_id)

    def is_expired(self, now: Optional[float] = None) -> bool:
        if self.remaining_ms <= 0:
            return True
        return (now if now is not None else time.monotonic()) >= self._deadline_monotonic


class DispatchContext(ExecutionContext):
    """别名：面向 handler 的执行上下文（= ExecutionContext）。"""


@dataclass(frozen=True)
class DispatchOutcome:
    """method handler 的返回值。用工厂方法构造，不要直接填字段。"""

    is_success: bool
    code: int
    payload: bytes = b""
    public_message: str = ""
    error_detail: bytes = b""

    @staticmethod
    def success(payload: bytes = b"") -> "DispatchOutcome":
        return DispatchOutcome(True, status.STATUS_CODE_OK, payload=payload)

    @staticmethod
    def accepted(payload: bytes = b"") -> "DispatchOutcome":
        """长任务已受理（§10.9）：成功以 ACCEPTED 应答；operation 终态由 nervud/reporter 走。"""
        return DispatchOutcome(True, status.STATUS_CODE_ACCEPTED, payload=payload)

    @staticmethod
    def failure(code: int, public_message: str = "", error_detail: bytes = b"") -> "DispatchOutcome":
        return DispatchOutcome(False, code, public_message=public_message, error_detail=error_detail)


# handler(payload, ctx) -> DispatchOutcome
MethodHandler = Callable[[bytes, ExecutionContext], DispatchOutcome]


@dataclass
class _MethodEntry:
    handler: MethodHandler
    requires_control_lease: bool
    is_motion: bool
    safety_latched_detail: bytes = b""
    stale_epoch_detail: bytes = b""


class NervusServiceHost:
    def __init__(
        self,
        sdk_name: str = "nervus-python-sdk",
        sdk_version: str = "0.1.0",
        *,
        min_protocol_major: int = 1,
        max_protocol_major: int = 1,
        max_protocol_minor: int = 0,
        safety_state: Optional[SafetyState] = None,
        operation_transport: Optional[OperationTransport] = None,
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
        self._register_pending = PendingMap()

        self._methods: Dict[int, _MethodEntry] = {}
        self._registrations: Dict[str, Registration] = {}
        self._cancelled_routes: set = set()
        self._lock = threading.Lock()

        self.safety_state = safety_state or SafetyState()
        self._operation_transport = operation_transport

        self._reader_thread: Optional[threading.Thread] = None
        self._ping_thread: Optional[threading.Thread] = None
        self._running = threading.Event()

    @property
    def connected(self) -> bool:
        return self._running.is_set()

    @property
    def limits(self) -> Optional[ipc.ConnectionLimits]:
        return self._handshake.limits if self._handshake else None

    # ---- method registration --------------------------------------------
    def register_method(
        self,
        method_id: int,
        handler: MethodHandler,
        *,
        requires_control_lease: bool = False,
        is_motion: bool = False,
        safety_latched_detail: bytes = b"",
        stale_epoch_detail: bytes = b"",
    ) -> None:
        """注册一个 method handler。

        requires_control_lease / is_motion 让 ServiceHost 就地做 fail-closed 复核
        （真相源是 Method Registry 的 method_meta；此处是 Provider 侧的复核开关，
        与 method_meta 语义对齐，不得自报调低风险）。

        safety_latched_detail / stale_epoch_detail：可选的**接口特定** typed
        error_detail bytes（位置 B）。由 Provider 用其接口 schema 的 *ErrorDetail
        消息序列化好传入（如 BaseMotionErrorDetail{SAFETY_LATCHED}）——核心 SDK
        不绑定任何具体接口、也不手搓 detail 字节。留空则只回通用 FAILED_PRECONDITION。
        """
        with self._lock:
            self._methods[method_id] = _MethodEntry(
                handler, requires_control_lease, is_motion, safety_latched_detail, stale_epoch_detail
            )

    def operation_reporter(
        self,
        operation_id: int,
        motion_epoch: int = 0,
        transport: Optional[OperationTransport] = None,
    ) -> OperationReporter:
        """为一个 nervud 拥有的 operation 建回报器（Accept/Progress/Succeed/Fail/Cancelled）。

        operation_id 由 nervud 分配（B1 落地后经 ExecutionContext.operation_id 传入），
        Provider 不得自签发。
        """
        return OperationReporter(operation_id, motion_epoch, transport or self._operation_transport)

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

        idle_ms = result.limits.idle_timeout_ms
        sock.settimeout(min(idle_ms / 1000.0, 1.0) if idle_ms > 0 else 1.0)
        self._reader_thread = threading.Thread(target=self._reader_loop, name="nervus-host-reader", daemon=True)
        self._reader_thread.start()
        if idle_ms > 0:
            self._ping_thread = threading.Thread(
                target=self._ping_loop, args=(idle_ms / 1000.0 / 2.0,), name="nervus-host-ping", daemon=True
            )
            self._ping_thread.start()
        return result

    def close(self) -> None:
        self._teardown(Disconnected("service host closed"), graceful=True)

    def __enter__(self) -> "NervusServiceHost":
        return self

    def __exit__(self, *exc) -> None:
        self.close()

    def _teardown(self, exc: BaseException, *, graceful: bool = False) -> None:
        if not self._running.is_set():
            return
        if graceful:
            # 优雅下线：先把每个 endpoint 撤下（SERVICE_SHUTTING_DOWN），再断连接。
            with self._lock:
                regs = list(self._registrations.values())
            for reg in regs:
                try:
                    self._write(
                        ipc.Envelope(
                            unregister_endpoint=ipc.UnregisterEndpoint(
                                request_id=self._ids.next(), endpoint_id=reg.endpoint_id
                            )
                        )
                    )
                except Exception:  # noqa: BLE001
                    pass
        self._running.clear()
        try:
            if self._sock is not None:
                self._sock.close()
        except OSError:
            pass
        self._register_pending.fail_all(lambda: exc)
        with self._lock:
            self._registrations.clear()
            self._cancelled_routes.clear()

    def _write(self, env: ipc.Envelope) -> None:
        writer = self._writer
        if writer is None or not self._running.is_set():
            raise NervusError("not connected")
        writer.write_frame(env)

    def _safe_write(self, env: ipc.Envelope) -> None:
        try:
            self._write(env)
        except Exception as e:  # noqa: BLE001
            _log.debug("write failed: %s", e)

    # ---- endpoint register / unregister ---------------------------------
    def register_endpoint(
        self,
        interface_id: str,
        interface_major: int,
        interface_minor: int,
        *,
        schema_hash: bytes = b"",
        resource_handle: str = "",
        timeout_ms: int = 5000,
    ) -> Registration:
        rid = self._ids.next()
        fut = self._register_pending.register(rid)
        reg_msg = ipc.RegisterEndpoint(
            request_id=rid,
            interface_id=interface_id,
            interface_major=interface_major,
            interface_minor=interface_minor,
            interface_schema_hash=schema_hash,
            resource_handle=resource_handle,
        )
        try:
            self._write(ipc.Envelope(register_endpoint=reg_msg))
        except Exception as e:  # noqa: BLE001
            self._register_pending.fail_one(rid, Disconnected(f"write failed: {e}"))
        result: ipc.RegisterEndpointResult = self._wait(rid, fut, timeout_ms)
        if result.WhichOneof("outcome") == "failure":
            f = result.failure
            raise CallFailed(f.code, f.public_message, bytes(f.error_detail))
        reg = Registration(result.success.endpoint_id, interface_id, interface_major, interface_minor)
        with self._lock:
            self._registrations[interface_id] = reg
        return reg

    def unregister_endpoint(self, endpoint_id: int, *, drain: bool = False, timeout_ms: int = 5000) -> None:
        rid = self._ids.next()
        fut = self._register_pending.register(rid)
        try:
            self._write(
                ipc.Envelope(
                    unregister_endpoint=ipc.UnregisterEndpoint(request_id=rid, endpoint_id=endpoint_id, drain=drain)
                )
            )
        except Exception as e:  # noqa: BLE001
            self._register_pending.fail_one(rid, Disconnected(f"write failed: {e}"))
        result: ipc.UnregisterEndpointResult = self._wait(rid, fut, timeout_ms)
        with self._lock:
            self._registrations = {k: v for k, v in self._registrations.items() if v.endpoint_id != endpoint_id}
        if result.WhichOneof("outcome") == "failure":
            f = result.failure
            raise CallFailed(f.code, f.public_message, bytes(f.error_detail))

    def _wait(self, request_id: int, fut, timeout_ms: int):
        from concurrent.futures import TimeoutError as FutureTimeoutError

        from .errors import Timeout

        try:
            return fut.result(timeout=(timeout_ms / 1000.0 + 1.0) if timeout_ms > 0 else None)
        except FutureTimeoutError as e:
            self._register_pending.discard(request_id)
            raise Timeout(f"request {request_id} timed out after {timeout_ms}ms") from e

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
                self._dispatch_envelope(env)
        except Exception as e:  # noqa: BLE001
            _log.debug("reader loop exited: %s", e)
        finally:
            self._teardown(Disconnected("connection closed by server"))

    def _dispatch_envelope(self, env: ipc.Envelope) -> None:
        body = env.WhichOneof("body")
        if body == "register_endpoint_result":
            self._register_pending.complete(env.register_endpoint_result.request_id, env.register_endpoint_result)
        elif body == "unregister_endpoint_result":
            self._register_pending.complete(env.unregister_endpoint_result.request_id, env.unregister_endpoint_result)
        elif body == "dispatch":
            self._handle_dispatch(env.dispatch)
        elif body == "cancel_dispatch":
            self._handle_cancel_dispatch(env.cancel_dispatch)
        elif body == "ping":
            self._safe_write(ipc.Envelope(pong=ipc.Pong(nonce=env.ping.nonce)))
        elif body == "pong":
            pass
        else:
            _log.error("unknown envelope body %r - closing connection", body)
            self._teardown(ProtocolViolation(f"unknown envelope body {body!r}"))

    def _handle_cancel_dispatch(self, cancel: ipc.CancelDispatch) -> None:
        with self._lock:
            self._cancelled_routes.add(cancel.route_id)
        _log.info("cancel dispatch route_id=%d reason=%s", cancel.route_id, cancel.reason)

    def _handle_dispatch(self, dispatch: ipc.Dispatch) -> None:
        result = self._run_dispatch(dispatch)
        self._safe_write(ipc.Envelope(dispatch_result=result))

    def _run_dispatch(self, dispatch: ipc.Dispatch) -> ipc.DispatchResult:
        route_id = dispatch.route_id

        with self._lock:
            entry = self._methods.get(dispatch.method_id)
            was_cancelled = route_id in self._cancelled_routes
            self._cancelled_routes.discard(route_id)

        if was_cancelled:
            return _fail(route_id, status.STATUS_CODE_CANCELLED, "dispatch cancelled")

        if entry is None:
            return _fail(route_id, status.STATUS_CODE_NOT_FOUND, f"unknown method_id: {dispatch.method_id}")

        ctx = ExecutionContext(
            route_id=route_id,
            endpoint_id=dispatch.endpoint_id,
            method_id=dispatch.method_id,
            remaining_ms=dispatch.remaining_ms,
            caller=dispatch.caller,
            _deadline_monotonic=time.monotonic() + max(0, dispatch.remaining_ms) / 1000.0,
        )

        # ---- fail-closed 复核（NRCP §10.4）：只拒绝，不裁决 ----
        # 1) 过 deadline 拒绝。
        if ctx.is_expired():
            return _fail(route_id, status.STATUS_CODE_DEADLINE_EXCEEDED, "deadline exceeded before dispatch")

        # 2) 缺 ExecutionContext（无 nervud 附加的 caller 身份）的控制调用拒绝。
        if entry.requires_control_lease and not ctx.has_caller_identity():
            return _fail(
                route_id,
                status.STATUS_CODE_FAILED_PRECONDITION,
                "control call without execution context",
            )

        # 3) 收 SafetyHalt 后不接普通运动调用。
        if entry.is_motion and self.safety_state.latched:
            return _fail(
                route_id,
                status.STATUS_CODE_FAILED_PRECONDITION,
                "safety latched",
                entry.safety_latched_detail,
            )

        # 4) 旧 motion_epoch 拒绝（B1 落地填 ctx.motion_epoch 后即生效）。
        if ctx.motion_epoch and self.safety_state.is_stale_epoch(ctx.motion_epoch):
            return _fail(
                route_id,
                status.STATUS_CODE_FAILED_PRECONDITION,
                "stale motion epoch",
                entry.stale_epoch_detail,
            )

        # ---- 调 handler（handler 只从 ctx.caller 取身份，绝不信 payload 里的 source）----
        try:
            outcome = entry.handler(bytes(dispatch.payload), ctx)
        except CallFailed as e:
            return _fail(route_id, e.code, e.public_message, e.error_detail)
        except Exception:  # noqa: BLE001 —— handler 异常归一化为 INTERNAL，不外泄细节
            _log.exception("handler for method %d raised", dispatch.method_id)
            return _fail(route_id, status.STATUS_CODE_INTERNAL, "handler error")

        return _build_result(route_id, outcome)

    def _ping_loop(self, interval: float) -> None:
        while self._running.is_set():
            deadline = time.monotonic() + interval
            while self._running.is_set() and time.monotonic() < deadline:
                time.sleep(min(0.1, max(0.0, deadline - time.monotonic())))
            if not self._running.is_set():
                break
            self._safe_write(ipc.Envelope(ping=ipc.Ping(nonce=self._ids.next())))


def _fail(route_id: int, code: int, message: str = "", detail: bytes = b"") -> ipc.DispatchResult:
    return ipc.DispatchResult(
        route_id=route_id,
        failure=status.Failure(code=code, public_message=message, error_detail=detail),
    )


def _build_result(route_id: int, outcome: DispatchOutcome) -> ipc.DispatchResult:
    if outcome.is_success:
        return ipc.DispatchResult(
            route_id=route_id,
            success=status.Success(code=outcome.code, payload=outcome.payload),
        )
    return _fail(route_id, outcome.code, outcome.public_message, outcome.error_detail)
