"""Operation 上报：状态机合法/非法转移 + 终态只写一次 + epoch 绑定（B5 语义）。"""

from __future__ import annotations

import threading

import pytest

from nervus.ipc.v1 import status_pb2 as status
from nervus_ipc import NullOperationTransport, OperationReporter, OperationState
from nervus_ipc.operation import OperationError


def test_happy_path_accept_progress_succeed():
    t = NullOperationTransport()
    r = OperationReporter(operation_id=1, motion_epoch=3, transport=t)
    assert r.state == OperationState.PENDING
    r.accept()
    assert r.state == OperationState.RUNNING
    r.progress(b"25%")
    r.progress(b"50%")
    r.succeed(b"done")
    assert r.state == OperationState.SUCCEEDED
    kinds = [c[0] for c in t.calls]
    assert kinds == ["accept", "progress", "progress", "succeed"]


def test_illegal_succeed_before_accept():
    r = OperationReporter(operation_id=2)
    with pytest.raises(OperationError):
        r.succeed()  # PENDING → SUCCEEDED 非法（须先 accept）


def test_terminal_written_once():
    r = OperationReporter(operation_id=3)
    r.accept()
    r.succeed()
    with pytest.raises(OperationError):
        r.fail()  # 已终结，异终态请求拒绝
    # 幂等：重复的同终态 no-op
    r.succeed()


def test_cancel_flow():
    r = OperationReporter(operation_id=4)
    r.accept()
    r.cancel_requested()
    assert r.state == OperationState.CANCEL_REQUESTED
    r.cancelled()
    assert r.state == OperationState.CANCELLED


def test_cancel_requested_can_be_preempted_by_success():
    r = OperationReporter(operation_id=5)
    r.accept()
    r.cancel_requested()
    r.succeed()  # 设备在取消送达前已完成 → 抢先终结
    assert r.state == OperationState.SUCCEEDED


def test_stale_epoch_rejected_on_accept():
    r = OperationReporter(operation_id=6, motion_epoch=10)
    with pytest.raises(OperationError):
        r.accept(motion_epoch=9)  # 陈旧世代


def test_concurrent_terminal_single_winner():
    r = OperationReporter(operation_id=7)
    r.accept()
    winners = []
    barrier = threading.Barrier(3)

    def try_terminal(fn):
        barrier.wait()
        try:
            fn()
            winners.append(fn.__name__)
        except OperationError:
            pass

    threads = [
        threading.Thread(target=try_terminal, args=(r.succeed,)),
        threading.Thread(target=try_terminal, args=(r.fail,)),
        threading.Thread(target=try_terminal, args=(r.cancelled,)),
    ]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    assert len(winners) == 1  # 恰一个胜者
    assert r.state in (OperationState.SUCCEEDED, OperationState.FAILED, OperationState.CANCELLED)
