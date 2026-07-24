"""Operation 上报（映射 B5 `ProviderReporter` 语义，经 nervud）。

Operation 是**系统协调长任务**的方式（机械臂轨迹/回零/移到位姿）：状态机与句柄由
**nervud 拥有**（B5 铁律 #1）。Provider 只经 nervud 回报进度/终态；本模块在 Python
侧提供与 B5 `ProviderReporter` 同名同义的接缝，并在本地强制：

  - 合法转移集合（B5 §3）——非法转移抛错；
  - **终态只写一次**（SUCCEEDED/FAILED/CANCELLED，CAS 语义，B5 铁律 #2、§9）；
  - epoch 绑定（Accept 校验 epoch，陈旧忽略）。

wire 现状（B5 §11）：operation 的 IPC proto（CreateOperation/OperationEvent/终态）
**目前不存在**。因此实际投递交给可插拔的 :class:`OperationTransport`；默认
:class:`NullOperationTransport` 只记录调用、不伪造 wire（fail-closed，标
`TODO(A-operation-proto)`）。B1 dispatch + operation proto 落地后换真 transport，
E 组 provider 代码面不变。这与内核 B5「先用本地类型 + TODO」的做法一致。
"""

from __future__ import annotations

import threading
from enum import IntEnum
from typing import List, Optional, Protocol, Tuple

from nervus.ipc.v1 import status_pb2 as status

from .errors import NervusError


class OperationState(IntEnum):
    UNSPECIFIED = 0
    PENDING = 1
    RUNNING = 2
    CANCEL_REQUESTED = 3
    SUCCEEDED = 4
    FAILED = 5
    CANCELLED = 6


_TERMINAL = frozenset({OperationState.SUCCEEDED, OperationState.FAILED, OperationState.CANCELLED})

# B5 §3 的「唯一允许集合」，其余一律拒绝。
_ALLOWED = {
    OperationState.PENDING: frozenset({OperationState.RUNNING, OperationState.FAILED, OperationState.CANCEL_REQUESTED}),
    OperationState.RUNNING: frozenset({OperationState.SUCCEEDED, OperationState.FAILED, OperationState.CANCEL_REQUESTED}),
    OperationState.CANCEL_REQUESTED: frozenset(
        {OperationState.SUCCEEDED, OperationState.FAILED, OperationState.CANCELLED}
    ),
}


class OperationError(NervusError):
    """非法 operation 转移，或对已终结 operation 的异终态请求。"""


class OperationTransport(Protocol):
    """把 Provider 的回报投递给 nervud。真实实现依赖 operation wire（尚未冻结）。"""

    def send_accept(self, operation_id: int, motion_epoch: int) -> None: ...
    def send_progress(self, operation_id: int, payload: bytes) -> None: ...
    def send_succeed(self, operation_id: int, result: bytes) -> None: ...
    def send_fail(self, operation_id: int, code: int, detail: bytes) -> None: ...
    def send_cancelled(self, operation_id: int) -> None: ...


class NullOperationTransport:
    """默认 transport：记录调用、不伪造 wire（operation wire 未冻结，B5 §11）。

    调用 `calls` 属性可在测试里断言回报序列。生产中在 operation wire 落地前，
    Provider 的进度/终态回报**不会真正上 wire**——这是有意的 fail-closed，
    而不是静默丢弃：ACCEPTED 这一步由 DispatchResult 表达（见 service_host），
    其余留待 `TODO(A-operation-proto)`。
    """

    def __init__(self) -> None:
        self.calls: List[Tuple[str, tuple]] = []

    def send_accept(self, operation_id: int, motion_epoch: int) -> None:
        self.calls.append(("accept", (operation_id, motion_epoch)))

    def send_progress(self, operation_id: int, payload: bytes) -> None:
        self.calls.append(("progress", (operation_id, bytes(payload))))

    def send_succeed(self, operation_id: int, result: bytes) -> None:
        self.calls.append(("succeed", (operation_id, bytes(result))))

    def send_fail(self, operation_id: int, code: int, detail: bytes) -> None:
        self.calls.append(("fail", (operation_id, int(code), bytes(detail))))

    def send_cancelled(self, operation_id: int) -> None:
        self.calls.append(("cancelled", (operation_id,)))


class OperationReporter:
    """Provider 侧回报器：本地强制 B5 状态机 + 终态只写一次，再委托 transport。

    由 ServiceHost 在遇到 operation-returning 方法时创建；`operation_id` 由 nervud
    分配（Provider **不得**自签发，B5 铁律 #1）。
    """

    def __init__(
        self,
        operation_id: int,
        motion_epoch: int = 0,
        transport: Optional[OperationTransport] = None,
    ) -> None:
        self.operation_id = operation_id
        self.motion_epoch = motion_epoch
        self._transport: OperationTransport = transport or NullOperationTransport()
        self._lock = threading.Lock()
        self._state = OperationState.PENDING

    @property
    def state(self) -> OperationState:
        with self._lock:
            return self._state

    def _set(self, target: OperationState) -> None:
        # CAS：只有当前非终态且转移合法才写；异终态请求对终态一律拒绝。
        if self._state in _TERMINAL:
            if self._state == target:
                return  # 幂等：重复的同终态请求当 no-op（B5 §3）
            raise OperationError(f"operation {self.operation_id} already terminal ({self._state.name})")
        if target not in _ALLOWED.get(self._state, frozenset()):
            raise OperationError(
                f"illegal transition {self._state.name} -> {target.name} for operation {self.operation_id}"
            )
        self._state = target

    def accept(self, motion_epoch: Optional[int] = None) -> None:
        """PENDING → RUNNING。epoch 陈旧（< 绑定 epoch）则拒绝（B5 epoch 绑定校验）。"""
        with self._lock:
            if motion_epoch is not None and self.motion_epoch and motion_epoch < self.motion_epoch:
                raise OperationError(
                    f"stale epoch {motion_epoch} < {self.motion_epoch} for operation {self.operation_id}"
                )
            self._set(OperationState.RUNNING)
            self._transport.send_accept(self.operation_id, self.motion_epoch)

    def progress(self, payload: bytes = b"") -> None:
        """typed 进度事件，**不改 state**（须处于 RUNNING/CANCEL_REQUESTED）。"""
        with self._lock:
            if self._state not in (OperationState.RUNNING, OperationState.CANCEL_REQUESTED):
                raise OperationError(f"progress not allowed in {self._state.name}")
            self._transport.send_progress(self.operation_id, payload)

    def cancel_requested(self) -> None:
        """PENDING/RUNNING → CANCEL_REQUESTED（收到取消；≠ 已取消）。"""
        with self._lock:
            self._set(OperationState.CANCEL_REQUESTED)

    def succeed(self, result: bytes = b"") -> None:
        with self._lock:
            self._set(OperationState.SUCCEEDED)
            self._transport.send_succeed(self.operation_id, result)

    def fail(self, code: int = status.STATUS_CODE_INTERNAL, detail: bytes = b"") -> None:
        with self._lock:
            self._set(OperationState.FAILED)
            self._transport.send_fail(self.operation_id, code, detail)

    def cancelled(self) -> None:
        with self._lock:
            self._set(OperationState.CANCELLED)
            self._transport.send_cancelled(self.operation_id)
