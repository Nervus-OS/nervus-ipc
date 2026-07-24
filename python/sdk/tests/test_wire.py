"""wire 分帧：roundtrip、零长度、超限、畸形 Envelope、半包/粘包重组。"""

from __future__ import annotations

import socket
import struct
import threading

import pytest

from nervus.ipc.v1 import envelope_pb2 as ipc
from nervus.ipc.v1 import status_pb2 as status
from nervus_ipc.errors import Disconnected, ProtocolViolation
from nervus_ipc.wire import MAX_FRAME_BYTES, FrameReader, FrameWriter


def _pair():
    a, b = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
    return a, b


def test_roundtrip():
    a, b = _pair()
    try:
        w = FrameWriter(a)
        r = FrameReader(b)
        env = ipc.Envelope(ping=ipc.Ping(nonce=12345))
        w.write_frame(env)
        got = r.read_frame()
        assert got.WhichOneof("body") == "ping"
        assert got.ping.nonce == 12345
    finally:
        a.close()
        b.close()


def test_zero_length_frame_is_violation():
    a, b = _pair()
    try:
        a.sendall(struct.pack(">I", 0))  # 长度前缀为 0
        r = FrameReader(b)
        with pytest.raises(ProtocolViolation):
            r.read_frame()
    finally:
        a.close()
        b.close()


def test_oversize_write_is_violation():
    a, b = _pair()
    try:
        w = FrameWriter(a)
        big = ipc.Envelope(response=ipc.Response(request_id=1, success=status.Success(payload=b"x" * (MAX_FRAME_BYTES + 10))))
        with pytest.raises(ProtocolViolation):
            w.write_frame(big)
    finally:
        a.close()
        b.close()


def test_oversize_length_prefix_is_violation():
    a, b = _pair()
    try:
        a.sendall(struct.pack(">I", MAX_FRAME_BYTES + 1))
        r = FrameReader(b)
        with pytest.raises(ProtocolViolation):
            r.read_frame()
    finally:
        a.close()
        b.close()


def test_malformed_envelope_is_violation():
    a, b = _pair()
    try:
        garbage = b"\xff\xff\xff\xff\x0f"  # 非法 tag/varint
        a.sendall(struct.pack(">I", len(garbage)) + garbage)
        r = FrameReader(b)
        with pytest.raises(ProtocolViolation):
            r.read_frame()
    finally:
        a.close()
        b.close()


def test_eof_midframe_is_disconnected():
    a, b = _pair()
    try:
        a.sendall(struct.pack(">I", 100))  # 声明 100 字节但立刻关
        a.close()
        r = FrameReader(b)
        with pytest.raises(Disconnected):
            r.read_frame()
    finally:
        b.close()


def test_half_packet_reassembles():
    a, b = _pair()
    try:
        env = ipc.Envelope(ping=ipc.Ping(nonce=777))
        body = env.SerializeToString()
        frame = struct.pack(">I", len(body)) + body

        def writer():
            # 分两次写：先前半，稍后后半 —— reader 必须重组
            a.sendall(frame[:3])
            import time

            time.sleep(0.1)
            a.sendall(frame[3:])

        t = threading.Thread(target=writer)
        t.start()
        r = FrameReader(b)
        got = r.read_frame()
        t.join()
        assert got.ping.nonce == 777
    finally:
        a.close()
        b.close()
