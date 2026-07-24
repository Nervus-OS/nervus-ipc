"""Safety：SafetyState 锁存/陈旧世代 + SafetyChannel 立即停 + 回 HaltAccepted。"""

from __future__ import annotations

import socket
import struct

from nervus.ipc.v1 import safety_pb2 as safety
from nervus_ipc import SafetyChannel, SafetyState


def test_safety_state_latch_rearm():
    s = SafetyState()
    assert not s.latched
    s.latch(motion_epoch=5)
    assert s.latched
    assert s.epoch == 5
    # 陈旧世代判定
    assert s.is_stale_epoch(4) is True
    assert s.is_stale_epoch(5) is False
    assert s.is_stale_epoch(0) is False  # 0 = 未提供
    s.rearm(motion_epoch=6)
    assert not s.latched
    assert s.epoch == 6  # 单调不回退


def test_safety_channel_immediate_stop_and_accept():
    a, b = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        state = SafetyState()
        stops = []

        def stop_device(halt: safety.SafetyHalt):
            stops.append(halt.motion_epoch)

        chan = SafetyChannel(a, state, on_halt=stop_device)
        # 直接注入一个 SafetyHalt，走与线上相同的处理路径（无需 start 读循环）
        chan.push_halt(safety.SafetyHalt(motion_epoch=9, reason=safety.HALT_REASON_OPERATOR_ESTOP))

        # 1) 设备被立刻停
        assert stops == [9]
        # 2) 本地镜像锁存
        assert state.latched
        assert state.epoch == 9
        # 3) 回了 HaltAccepted（裸 Safety 消息，走本通道）
        b.settimeout(2.0)
        header = b.recv(4)
        (length,) = struct.unpack(">I", header)
        body = b.recv(length)
        ack = safety.HaltAccepted()
        ack.ParseFromString(body)
        assert ack.motion_epoch == 9
    finally:
        a.close()
        b.close()


def test_safety_channel_latches_even_if_stop_raises():
    a, b = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        state = SafetyState()

        def bad_stop(halt):
            raise RuntimeError("device stop failed")

        chan = SafetyChannel(a, state, on_halt=bad_stop)
        try:
            chan.push_halt(safety.SafetyHalt(motion_epoch=3))
        except RuntimeError:
            pass
        # fail-closed：即使停设备回调抛错，也必须锁存
        assert state.latched
        assert state.epoch == 3
    finally:
        a.close()
        b.close()
