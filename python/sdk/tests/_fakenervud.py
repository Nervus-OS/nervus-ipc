"""进程内 fake nervud，用真 AF_UNIX socket 驱动 client / ServiceHost 冒烟测试。

这是**测试替身**，不是 SDK 的一部分：它复用 SDK 的 wire 分帧（同仓库，同 128KiB/
length-prefix 约定）与生成的 protobuf，扮演 nervud 的一侧——完成 Hello 握手，然后
按测试脚本收发 Envelope。不做任何权限/lease/Safety 裁决（那些在真 nervud）。
"""

from __future__ import annotations

import os
import queue
import socket
import tempfile
import threading
from typing import Callable, Optional

from nervus.ipc.v1 import envelope_pb2 as ipc
from nervus_ipc.wire import FrameReader, FrameWriter


def default_limits() -> ipc.ConnectionLimits:
    # idle_timeout_ms=0 关掉 ping 线程，让冒烟测试确定性。
    return ipc.ConnectionLimits(
        max_frame_bytes=128 * 1024,
        default_method_payload_bytes=16 * 1024,
        max_inflight_requests=64,
        max_inflight_payload_bytes=1024 * 1024,
        max_outbound_queue_bytes=512 * 1024,
        max_subscriptions=64,
        default_timeout_ms=5000,
        max_timeout_ms=30000,
        idle_timeout_ms=0,
    )


class _ServerConn:
    def __init__(self, sock: socket.socket) -> None:
        self._sock = sock
        self._reader = FrameReader(sock)
        self._writer = FrameWriter(sock)

    def send(self, env: ipc.Envelope) -> None:
        self._writer.write_frame(env)

    def read(self) -> ipc.Envelope:
        return self._reader.read_frame()


class FakeNervud:
    """
    用法一（自动应答，适合 client 测试）：
        FakeNervud(on_frame=lambda conn, env: ...)  # on_frame 里按 env 回帧
    用法二（手动驱动，适合 ServiceHost 测试）：
        nv.wait_ready(); nv.send(dispatch_env); result = nv.recv()
    两种可混用：每个收到的非握手帧都进 inbox，同时（若提供）回调 on_frame。
    """

    def __init__(
        self,
        on_frame: Optional[Callable[["_ServerConn", ipc.Envelope], None]] = None,
        *,
        limits: Optional[ipc.ConnectionLimits] = None,
        package_id: str = "test.pkg",
        component_id: str = "test.comp",
        protocol_major: int = 1,
        protocol_minor: int = 0,
    ) -> None:
        self._on_frame = on_frame
        self._limits = limits or default_limits()
        self._package_id = package_id
        self._component_id = component_id
        self._pmaj = protocol_major
        self._pmin = protocol_minor

        self._dir = tempfile.mkdtemp(prefix="nervus-fake-")
        self.path = os.path.join(self._dir, "nervud.sock")
        self._srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self._srv.bind(self.path)
        self._srv.listen(1)
        self._srv.settimeout(5.0)

        self.inbox: "queue.Queue[ipc.Envelope]" = queue.Queue()
        self._ready = threading.Event()
        self._running = threading.Event()
        self.conn: Optional[_ServerConn] = None
        self._thread: Optional[threading.Thread] = None

    def start(self) -> "FakeNervud":
        self._running.set()
        self._thread = threading.Thread(target=self._serve, name="fake-nervud", daemon=True)
        self._thread.start()
        return self

    def _serve(self) -> None:
        try:
            client, _ = self._srv.accept()
        except (socket.timeout, OSError):
            return
        client.settimeout(1.0)
        conn = _ServerConn(client)
        self.conn = conn

        # 握手：收 Hello，回 HelloAck success。
        try:
            env = self._read_blocking(conn)
        except Exception:
            return
        if env is None or env.WhichOneof("body") != "hello":
            return
        conn.send(
            ipc.Envelope(
                hello_ack=ipc.HelloAck(
                    success=ipc.HelloAckSuccess(
                        protocol_major=self._pmaj,
                        protocol_minor=self._pmin,
                        package_id=self._package_id,
                        component_id=self._component_id,
                        limits=self._limits,
                    )
                )
            )
        )
        self._ready.set()

        while self._running.is_set():
            try:
                env = conn.read()
            except socket.timeout:
                continue
            except Exception:
                break
            if env.WhichOneof("body") == "ping":
                conn.send(ipc.Envelope(pong=ipc.Pong(nonce=env.ping.nonce)))
                continue
            self.inbox.put(env)
            if self._on_frame is not None:
                try:
                    self._on_frame(conn, env)
                except Exception:  # noqa: BLE001
                    break

    def _read_blocking(self, conn: _ServerConn):
        while self._running.is_set():
            try:
                return conn.read()
            except socket.timeout:
                continue
            except Exception:
                return None
        return None

    def wait_ready(self, timeout: float = 5.0) -> bool:
        return self._ready.wait(timeout)

    def send(self, env: ipc.Envelope) -> None:
        assert self.conn is not None, "not connected yet; call wait_ready()"
        self.conn.send(env)

    def recv(self, timeout: float = 5.0) -> ipc.Envelope:
        return self.inbox.get(timeout=timeout)

    def stop(self) -> None:
        self._running.clear()
        try:
            self._srv.close()
        except OSError:
            pass
        try:
            if self.conn is not None:
                self.conn._sock.close()
        except OSError:
            pass
        try:
            os.unlink(self.path)
        except OSError:
            pass
        try:
            os.rmdir(self._dir)
        except OSError:
            pass

    def __enter__(self) -> "FakeNervud":
        return self.start()

    def __exit__(self, *exc) -> None:
        self.stop()
