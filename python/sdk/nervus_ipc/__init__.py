"""Nervus IPC v1 —— Python SDK（client + ServiceHost）。

C 组交付（语言决策 2026-07-24）：ROS provider / 感知 / Agent 用 Python，需要 Python 版
IPC SDK。本包**复用** `nervus-ipc` 冻结 proto 生成的 protobuf 类型编解码 Envelope，
**绝不手搓信封**（总协调红线 #3，与 Kotlin SDK 同一做法）。

依赖方向（README §10.10）：
    ROS provider / 感知 / Agent(Python) → nervus-ipc/python/sdk → nervus-ipc/python/protocol

行为与 Kotlin SDK（`nervus-app-sdk`）保持一致：同一 A5 golden vectors 也过 Python 一遍，
防三语言漂移；Python 侧**不做任何安全裁决**——权限 / lease / Safety 都在 nervud，
ServiceHost 只对 ExecutionContext 做 fail-closed 复核。

生成物导入约定
--------------
生成的 protobuf（`nervus.ipc.v1.*_pb2` 等）落在与本包并列的 `python/protocol`
（buf `out`，PEP 420 命名空间包，无 __init__.py）。正式安装时它作为独立源根在
sys.path 上；未安装、直接用仓库布局跑测试/开发时，下面的 bootstrap 把兄弟目录
`../../protocol` 追加进 sys.path，使 `from nervus.ipc.v1 import envelope_pb2` 可用。
这是 monorepo protobuf 的常见便利，不改变「生成物属于 python/protocol」这一事实。
"""

from __future__ import annotations

import os as _os
import sys as _sys


def _ensure_generated_on_path() -> None:
    """若生成的 protobuf 包不可导入，把仓库布局下的兄弟 protocol 目录挂上 sys.path。"""
    try:  # 已安装 / 已在 PYTHONPATH → 什么都不做
        import nervus.ipc.v1.envelope_pb2  # noqa: F401
        return
    except Exception:  # noqa: BLE001 —— 任何导入失败都退回到布局探测
        pass
    here = _os.path.dirname(_os.path.abspath(__file__))
    protocol_root = _os.path.normpath(_os.path.join(here, "..", "..", "protocol"))
    if _os.path.isdir(_os.path.join(protocol_root, "nervus")) and protocol_root not in _sys.path:
        _sys.path.insert(0, protocol_root)


_ensure_generated_on_path()

from .client import ControlLease, NervusClient, ResolvedEndpoint  # noqa: E402
from .subscription import Subscription  # noqa: E402
from .errors import (  # noqa: E402
    CallFailed,
    Disconnected,
    HandshakeRejected,
    NervusError,
    ProtocolViolation,
    Timeout,
    status_name,
    typed_reason,
)
from .handshake import HandshakeResult  # noqa: E402
from .operation import (  # noqa: E402
    NullOperationTransport,
    OperationReporter,
    OperationState,
    OperationTransport,
)
from .safety import SafetyChannel, SafetyState  # noqa: E402
from .service_host import (  # noqa: E402
    DispatchContext,
    DispatchOutcome,
    ExecutionContext,
    MethodHandler,
    NervusServiceHost,
    Registration,
)

__all__ = [
    # client
    "NervusClient",
    "ResolvedEndpoint",
    "ControlLease",
    "Subscription",
    # service host
    "NervusServiceHost",
    "Registration",
    "ExecutionContext",
    "DispatchContext",
    "DispatchOutcome",
    "MethodHandler",
    # operation
    "OperationReporter",
    "OperationState",
    "OperationTransport",
    "NullOperationTransport",
    # safety
    "SafetyChannel",
    "SafetyState",
    # handshake / errors
    "HandshakeResult",
    "NervusError",
    "ProtocolViolation",
    "HandshakeRejected",
    "CallFailed",
    "Disconnected",
    "Timeout",
    "status_name",
    "typed_reason",
]

__version__ = "0.1.0"
