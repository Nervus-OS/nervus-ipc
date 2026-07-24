"""连接内的 request_id 生成与 pending 关联（§10.6）。"""

from __future__ import annotations

import threading
from concurrent.futures import Future
from typing import Callable, Dict

from .errors import Disconnected, NervusError

_U64_MAX = 0xFFFFFFFFFFFFFFFF


class RequestIdGenerator:
    """连接作用域的原子递增 request_id：0 永久保留，首个为 1，绝不回绕（§10.6）。

    达到 uint64 上限即抛错——调用者应关闭并重建连接，而不是回绕复用（回绕会让
    迟到响应错配到新请求）。
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._n = 0

    def next(self) -> int:
        with self._lock:
            if self._n >= _U64_MAX:
                raise NervusError("request_id space exhausted; reconnect required")
            self._n += 1
            return self._n


class PendingMap:
    """request_id → Future 的线程安全映射。

    匹配键在真实系统里是 (连接, request_id)；单连接内 request_id 唯一，故这里只用
    request_id。所有等待中的 Future 在断线时以 Disconnected 完结（§10.12）。
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._m: Dict[int, Future] = {}

    def register(self, request_id: int) -> Future:
        fut: Future = Future()
        with self._lock:
            if request_id in self._m:
                raise NervusError(f"duplicate in-flight request_id {request_id}")
            self._m[request_id] = fut
        return fut

    def complete(self, request_id: int, value: object) -> bool:
        with self._lock:
            fut = self._m.pop(request_id, None)
        if fut is None:
            return False
        if not fut.done():
            fut.set_result(value)
        return True

    def fail_one(self, request_id: int, exc: BaseException) -> None:
        with self._lock:
            fut = self._m.pop(request_id, None)
        if fut is not None and not fut.done():
            fut.set_exception(exc)

    def discard(self, request_id: int) -> None:
        """丢弃条目但不完结（用于 Cancel：终结 Response 仍会正常到达）。"""
        with self._lock:
            self._m.pop(request_id, None)

    def fail_all(self, exc_factory: Callable[[], BaseException] = lambda: Disconnected("connection closed")) -> None:
        with self._lock:
            items = list(self._m.items())
            self._m.clear()
        for _rid, fut in items:
            if not fut.done():
                fut.set_exception(exc_factory())

    def size(self) -> int:
        with self._lock:
            return len(self._m)
