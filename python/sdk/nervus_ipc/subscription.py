"""订阅：把服务端推送的 Event 变成可迭代/可回调的流（§10.8）。

订阅是**推送**不是轮询。DeliveryClass 决定 sequence 缺口的含义（见 envelope.proto）：
RELIABLE 不得静默丢弃、STATE 只保最新、LOSSY 可丢弃；`Event.dropped` 给出缺口数量。
本类只搬运 Event，语义判断交给消费者按 `delivery_class` 处理。
"""

from __future__ import annotations

import queue
from typing import Callable, Iterator, Optional

from nervus.ipc.v1 import envelope_pb2 as ipc

_CLOSED = object()  # 结束哨兵


class Subscription:
    def __init__(self, subscription_id: int, delivery_class: int, maxsize: int = 0) -> None:
        self.subscription_id = subscription_id
        self.delivery_class = delivery_class
        self._q: "queue.Queue" = queue.Queue(maxsize=maxsize)
        self._closed = False
        self.closed_reason: Optional[int] = None
        self._unsubscribe_fn: Optional[Callable[[], None]] = None

    # ---- 内部：由 client reader 线程调用 ----
    def _push(self, event: ipc.Event) -> None:
        if not self._closed:
            self._q.put(event)

    def _close(self, reason: Optional[int] = None) -> None:
        if self._closed:
            return
        self._closed = True
        self.closed_reason = reason
        self._q.put(_CLOSED)

    # ---- 消费者 API ----
    @property
    def closed(self) -> bool:
        return self._closed

    def __iter__(self) -> Iterator[ipc.Event]:
        return self

    def __next__(self) -> ipc.Event:
        item = self._q.get()
        if item is _CLOSED:
            raise StopIteration
        return item

    def next_event(self, timeout: Optional[float] = None) -> Optional[ipc.Event]:
        """取下一条事件；超时或订阅已关返回 None。"""
        try:
            item = self._q.get(timeout=timeout)
        except queue.Empty:
            return None
        if item is _CLOSED:
            return None
        return item

    def close(self) -> None:
        """退订：发 Unsubscribe（若已绑定），并结束本地迭代。"""
        fn = self._unsubscribe_fn
        if fn is not None and not self._closed:
            try:
                fn()
            except Exception:  # noqa: BLE001 —— 退订尽力而为
                self._close()
        else:
            self._close()
