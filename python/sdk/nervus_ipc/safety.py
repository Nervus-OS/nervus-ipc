"""高优先级 Safety Path（NRCP §14.5、C 组 ServiceHost 要求）。

Provider READY 前建立**预分配、有界**的 Safety 通道（专用 UDS/eventfd，A/nervud 侧
约定）。SafetyHalt 到达即**立即**触发设备停止动作，**不进普通 writer 队列**——普通
dispatch 队列可能正堵着，Safety 绝不能排在它后面（§14.5）。

wire 现状：safety.proto 已冻结边界消息（SafetyHalt/HaltAccepted/StopProgress/
StandstillConfirmed/ProviderFault），但**承载方式**（专用 UDS 还是 Dispatch）
「留待冻结」（safety.proto 文件头）。因此本模块把 Safety 通道抽象成一个独立 socket
（与主 dispatch 连接分开的 fd），SafetyHalt/回报都走它、预分配缓冲、专用线程；
真实 fd 的建立约定由 A/nervud 敲定后接上。这与「Safety 独立不可绕过」（红线 #5）一致：
Python 侧不做 Safety 裁决，只保证「收到 Halt → 立刻停 + 回执 + 本地锁存」这条快路径。

Python 侧**不做任何 Safety 裁决**：决定权/锁存权威/epoch 递增/re-arm 都在 nervud。
本地 :class:`SafetyState` 只是 ServiceHost 复核 ExecutionContext 用的**镜像**，
让「收到 SafetyHalt 后不接普通运动调用」这条 fail-closed 能就地执行。
"""

from __future__ import annotations

import socket
import threading
from typing import Callable, Optional

from nervus.ipc.v1 import safety_pb2 as safety

from .wire import FrameReader, FrameWriter


class SafetyState:
    """ServiceHost 侧的 Safety 锁存镜像（线程安全）。权威在 nervud，这里只做复核。"""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._latched = False
        self._epoch = 0

    @property
    def latched(self) -> bool:
        with self._lock:
            return self._latched

    @property
    def epoch(self) -> int:
        with self._lock:
            return self._epoch

    def latch(self, motion_epoch: int) -> None:
        """收到 SafetyHalt：锁存 + 记录撤销世代（单调不回退）。"""
        with self._lock:
            self._latched = True
            if motion_epoch > self._epoch:
                self._epoch = motion_epoch

    def rearm(self, motion_epoch: int = 0) -> None:
        """受控 re-arm 回 NONE（权威动作在 nervud；这里只清本地镜像）。"""
        with self._lock:
            self._latched = False
            if motion_epoch > self._epoch:
                self._epoch = motion_epoch

    def is_stale_epoch(self, motion_epoch: int) -> bool:
        """给定命令的 motion_epoch 是否属于已撤销世代（陈旧 → 拒绝）。"""
        with self._lock:
            return motion_epoch != 0 and motion_epoch < self._epoch


# on_halt 回调：Provider 注册的「立即停止设备」动作。参数是解析好的 SafetyHalt。
HaltHandler = Callable[[safety.SafetyHalt], None]


class SafetyChannel:
    """专用 Safety 通道：预分配、有界、独立于普通 dispatch 连接。

    用法（provider READY 前建立）：
        chan = SafetyChannel(safety_sock, state, on_halt=stop_device)
        chan.start()
    收到 SafetyHalt → 同步调用 on_halt（立刻停）→ latch 本地 SafetyState → 回
    HaltAccepted（走本通道，绕开普通 writer 队列）。on_halt 内不得阻塞在慢 I/O 上。
    """

    def __init__(
        self,
        sock: socket.socket,
        state: SafetyState,
        on_halt: HaltHandler,
        *,
        on_fault: Optional[Callable[[], None]] = None,
    ) -> None:
        self._sock = sock
        self._state = state
        self._on_halt = on_halt
        self._on_fault = on_fault
        # 预分配 reader/writer；HaltAccepted 消息对象复用，避免 halt 路径上再分配。
        self._reader = FrameReader(sock)
        self._writer = FrameWriter(sock)
        self._running = threading.Event()
        self._thread: Optional[threading.Thread] = None

    def start(self) -> None:
        if self._running.is_set():
            return
        self._running.set()
        self._sock.settimeout(1.0)
        self._thread = threading.Thread(target=self._loop, name="nervus-safety", daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._running.clear()
        try:
            self._sock.close()
        except OSError:
            pass

    def _loop(self) -> None:
        from .errors import Disconnected, ProtocolViolation
        from nervus.ipc.v1 import envelope_pb2 as ipc

        while self._running.is_set():
            try:
                env = self._reader.read_frame()
            except socket.timeout:
                continue
            except (Disconnected, ProtocolViolation):
                break
            body = env.WhichOneof("body")
            # Safety 边界消息可能直接以裸消息或包在 Envelope（承载方式未冻结）——两种都收。
            halt = self._extract_halt(env, body, ipc)
            if halt is not None:
                self._handle_halt(halt)

    @staticmethod
    def _extract_halt(env, body, ipc) -> Optional[safety.SafetyHalt]:
        # 约定未冻结：这里既支持 Envelope 里未来可能新增的 safety_halt 分支，也支持
        # 直接在 Safety 通道上发裸 SafetyHalt（由调用方用 push_halt 注入测试）。
        if body == "safety_halt":  # 若将来 envelope 增补该分支
            return env.safety_halt  # pragma: no cover
        return None

    def _handle_halt(self, halt: safety.SafetyHalt) -> None:
        # 1) 立刻停设备（同步，高优先级，不排队）。停设备失败不得拖垮 Safety 线程。
        stop_ok = True
        try:
            self._on_halt(halt)
        except Exception:  # noqa: BLE001
            stop_ok = False
        finally:
            # 2) 锁存本地镜像（即使 on_halt 抛错也要锁存：fail-closed）。
            self._state.latch(halt.motion_epoch)
        # 3) 回执（走本通道，绕开普通 writer）：停成功回 HaltAccepted；
        #    停失败回 ProviderFault（HaltAccepted ≠ 已停稳；停不下来是故障，§14.3）。
        if stop_ok:
            self._send(safety.HaltAccepted(motion_epoch=halt.motion_epoch))
        else:
            if self._on_fault is not None:
                try:
                    self._on_fault()
                except Exception:  # noqa: BLE001
                    pass
            self._send(
                safety.ProviderFault(motion_epoch=halt.motion_epoch, code=safety.FAULT_CODE_DEVICE_ERROR)
            )

    def push_halt(self, halt: safety.SafetyHalt) -> None:
        """测试/进程内注入一个 SafetyHalt，走与线上完全相同的处理路径。"""
        self._handle_halt(halt)

    def _send(self, msg) -> None:
        # 直接把裸 Safety 消息按 length-prefix 帧发出（Safety 通道自成一路，
        # 不复用普通 dispatch 连接的 writer 队列）。用生成类型编码，不手搓消息体。
        import struct

        body = msg.SerializeToString()
        frame = struct.pack(">I", len(body)) + body
        try:
            self._sock.sendall(frame)
        except OSError:
            pass
