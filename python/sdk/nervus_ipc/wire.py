"""控制面 wire：length-prefix 分帧 + 复用生成的 Envelope 编解码（§10.1、§10.3）。

传输组合固定为：
    AF_UNIX / SOCK_STREAM
      + uint32 big-endian length prefix（N 不含前缀自身，0 与 >128KiB 均为协议错误）
      + Protobuf Envelope

只有 4 字节长度前缀是本层「手写」的分帧；Envelope 本体一律用生成的 protobuf
序列化/解析，**绝不手搓信封字节**（红线 #3）。与 Kotlin FrameReader/FrameWriter
逐条对齐：零长度帧、超 128KiB、畸形 Envelope 都判协议违规。
"""

from __future__ import annotations

import socket
import struct
import threading

from nervus.ipc.v1 import envelope_pb2 as ipc

from .errors import Disconnected, ProtocolViolation

HEADER_SIZE = 4
# 绝对 Frame 上限，恒为 131072（128 KiB），对齐 ConnectionLimits.max_frame_bytes（§10.3）。
MAX_FRAME_BYTES = 128 * 1024

_HEADER = struct.Struct(">I")  # uint32 big-endian


def _recv_exact(sock: socket.socket, n: int) -> bytes:
    """从阻塞 socket 精确读 n 字节。EOF → Disconnected；socket.timeout 向上抛。"""
    chunks: list[bytes] = []
    remaining = n
    while remaining > 0:
        b = sock.recv(remaining)
        if not b:
            raise Disconnected("connection closed unexpectedly")
        chunks.append(b)
        remaining -= len(b)
    if len(chunks) == 1:
        return chunks[0]
    return b"".join(chunks)


class FrameReader:
    """从 socket 逐帧读取并解析成 Envelope。非线程安全：仅 reader 线程独占使用。"""

    def __init__(self, sock: socket.socket) -> None:
        self._sock = sock

    def read_frame(self) -> ipc.Envelope:
        header = _recv_exact(self._sock, HEADER_SIZE)
        (length,) = _HEADER.unpack(header)
        if length == 0:
            # 空 Envelope 没有任何合法用途，容忍它只会给「发一堆空帧刷预算」留口子。
            raise ProtocolViolation("zero-length frame")
        if length > MAX_FRAME_BYTES:
            raise ProtocolViolation(f"frame too large: {length} > {MAX_FRAME_BYTES}")
        body = _recv_exact(self._sock, length)
        env = ipc.Envelope()
        try:
            env.ParseFromString(body)
        except Exception as e:  # noqa: BLE001 —— protobuf 解析失败一律视为畸形帧
            raise ProtocolViolation(f"malformed envelope: {e}") from e
        return env


class FrameWriter:
    """把 Envelope 序列化成 length-prefix 帧写出。线程安全：一把锁保证帧不交错。"""

    def __init__(self, sock: socket.socket) -> None:
        self._sock = sock
        self._lock = threading.Lock()

    def write_frame(self, env: ipc.Envelope) -> None:
        body = env.SerializeToString()
        length = len(body)
        if length > MAX_FRAME_BYTES:
            raise ProtocolViolation(f"frame too large to write: {length} > {MAX_FRAME_BYTES}")
        frame = _HEADER.pack(length) + body
        with self._lock:
            self._sock.sendall(frame)


def connect_unix(socket_path: str) -> socket.socket:
    """连接到 nervud 的 AF_UNIX SOCK_STREAM 控制面 socket。

    身份由 nervud 侧经 SO_PEERCRED 核验（PID/UID/GID → Package），Python 侧不自报，
    Hello 里也没有任何可被相信的身份字段（§10.2）。
    """
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        sock.connect(socket_path)
    except OSError:
        sock.close()
        raise
    return sock
