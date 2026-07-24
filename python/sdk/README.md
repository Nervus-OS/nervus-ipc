# nervus-ipc · Python SDK（client + ServiceHost）

Nervus OS 控制面 IPC v1 的 **Python** SDK。C 组交付（语言决策 2026-07-24）：ROS
adapter / 感知 / Agent 用 Python，需要 Python 版 IPC SDK；**Go SDK 本轮不做**，
provider/服务一律不用 Go。

本 SDK **复用** `nervus-ipc` 冻结 proto 生成的 protobuf 类型编解码 Envelope，
**绝不手搓信封**（总协调红线 #3，与 Kotlin `nervus-app-sdk` 同一做法）。行为与
Kotlin SDK 对齐，并与 Go/JVM 过**同一份 A5 golden vectors**，防三语言漂移。

## 布局

```
python/
├── protocol/         ← 生成物（buf out；PEP 420 命名空间包，clean 会整目录删）
│   └── nervus/ipc/v1/*_pb2.py  …  com/acme/... 各接口
└── sdk/
    ├── nervus_ipc/   ← 手写 SDK（本包）
    │   ├── wire.py           length-prefix 分帧（复用生成 Envelope，不手搓）
    │   ├── handshake.py      Hello / HelloAck
    │   ├── errors.py         StatusCode + typed error_detail 解码
    │   ├── rpc.py            request_id 生成 + pending future
    │   ├── client.py         NervusClient（消费侧）
    │   ├── subscription.py   订阅事件流
    │   ├── service_host.py   NervusServiceHost（提供侧）+ ExecutionContext + fail-closed
    │   ├── operation.py      OperationReporter（映射 B5 ProviderReporter 语义）
    │   └── safety.py         SafetyState + 高优先级 SafetyChannel
    └── tests/         冒烟 + 单测 + golden vectors（Python 侧）
```

依赖方向（README §10.10）：
`ROS provider / 感知 / Agent(Python)` → `python/sdk` → `python/protocol`。

## 生成 protobuf

Python 类型随 `buf generate` 与 Go/Java/Kotlin **一起**产出（`buf.gen.yaml` 增补了
`protoc_builtin: python` + `pyi`，落 `python/protocol`）。一个 commit 里躺四语言产物，
CI 一条 `buf generate && git diff --exit-code` 同时覆盖（NRCP §22.6）。

```sh
buf lint
buf generate            # 需要 PATH 上有 protoc（java/kotlin/python 内置生成器）+ protoc-gen-go
```

## 安装与运行

生成物 `python/protocol` 是**独立源根**（namespace 包，无 `__init__.py`）。三种用法：

1. **仓库内直接跑**（推荐开发/测试）：把 `python/sdk` 放上 `PYTHONPATH`；
   `nervus_ipc/__init__.py` 会自动把兄弟 `../protocol` 挂上。
   ```sh
   PYTHONPATH=python/sdk python3 -c "import nervus_ipc"
   ```
2. **editable 安装**：`pip install -e python`（源码原地，`__init__` bootstrap 仍生效）。
3. **打包分发**：把 `python/protocol` 一并纳入分发的 PYTHONPATH（generated code 提交进仓库，
   消费者无需工具链）。

运行时依赖：`protobuf>=5.29,<6`（与 protoc 29.3 生成代码对齐）。

## 用法

### client（消费侧）

```python
from nervus_ipc import NervusClient
from nervus.ipc.v1 import envelope_pb2 as ipc

with NervusClient() as c:
    c.connect("/run/nervus/nervud.sock", component_id="com.acme.app")
    ep = c.resolve_endpoint("nervus.interface.motion.base",
                            resource_type="nervus.resource.motion.base", resource_role="main")
    lease = c.acquire_control(ipc.CONTROLLER_CLASS_HUMAN,
                              resource_type="nervus.resource.motion.base", resource_role="main")
    resp = c.call(ep.endpoint_id, method_id=1, payload=b"...")   # 返回 Response（Success/Failure）
    for ev in c.subscribe(ep.endpoint_id, event_id=1):
        ...
    c.release_control(lease.lease_id)
```

错误解码（NRCP §19）：先看通用 `StatusCode`（`CallFailed.code`），再按方法 detail 类型
用 `typed_reason(error_detail, DetailCls)` 解 typed reason；**未知 reason 保留通用 code、
不判协议损坏**（golden vector `*_unknown_reason`）。

### ServiceHost（提供侧）

```python
from nervus_ipc import NervusServiceHost, DispatchOutcome

host = NervusServiceHost()
host.connect("/run/nervus/nervud.sock", component_id="com.acme.provider")

def set_velocity(payload: bytes, ctx) -> DispatchOutcome:
    # 身份只来自 ctx.caller（nervud 附加），绝不从 payload 读 source=HUMAN 之类
    ...
    return DispatchOutcome.success()

host.register_method(2, set_velocity, requires_control_lease=True, is_motion=True)
host.register_endpoint("nervus.interface.motion.base", 1, 0, resource_handle="base.main")
```

**fail-closed 复核**（NRCP §10.4，SDK 只复核不裁决）：收到 Dispatch 后、调 handler 前拒绝——
- 过 deadline（`remaining_ms<=0` 或单调 deadline 已过）→ `DEADLINE_EXCEEDED`；
- 缺 ExecutionContext（无 nervud 附加 caller 身份）的控制调用 → `FAILED_PRECONDITION`；
- 收到 SafetyHalt 后（本地 `SafetyState` 锁存）的普通运动调用 → `FAILED_PRECONDITION`；
- 旧 `motion_epoch`（陈旧世代）→ `FAILED_PRECONDITION`（B1 把 epoch 附到 Dispatch 后即生效）；
- 未知 `method_id` → `NOT_FOUND`；被 `CancelDispatch` 标记的 route → `CANCELLED`。

> ExecutionContext 全集（caller/resource/lease/epoch/deadline/operation_id）中，
> lease/epoch/operation_id 是 nervud **B1 dispatch** 落地后才附到 Dispatch wire 的字段；
> 本 SDK 先留字段、缺省 0，用「已在 wire 上的」那部分执行 fail-closed，B1 补齐后其余校验
> 即刻生效，**接口面不变**。

### operation 上报（映射 B5 ProviderReporter）

长任务（机械臂轨迹/回零/移到位姿）以 `DispatchOutcome.accepted()` 应答（`ACCEPTED`），
再用 `OperationReporter` 回报 `accept/progress/succeed/fail/cancelled`（本地强制 B5 状态机
+ 终态只写一次 + epoch 绑定）。

> **wire 现状**（B5 §11）：operation 的 IPC proto（CreateOperation/OperationEvent/终态）
> **目前不存在**。因此进度/终态经可插拔 `OperationTransport` 投递，默认 `NullOperationTransport`
> 只记录、不伪造 wire（fail-closed，标 `TODO(A-operation-proto)`）。`ACCEPTED` 这一步由
> DispatchResult 表达。operation proto + B1 落地后换真 transport，provider 代码面不变——
> 与内核 B5「先用本地类型 + TODO」一致。

### 高优先级 Safety Path

provider READY 前建立**预分配、有界**的 `SafetyChannel`（专用 fd，独立于普通 dispatch 连接）。
`SafetyHalt` 到达即**立刻**触发设备停止 + 锁存本地 `SafetyState` + 回 `HaltAccepted`（停失败回
`ProviderFault`），**不进普通 writer 队列**（NRCP §14.5）。

> **承载方式未冻结**：safety.proto 冻结了边界消息（SafetyHalt/HaltAccepted/…），但走专用 UDS
> 还是 Dispatch「留待冻结」。本 SDK 把 Safety 通道抽象成独立 socket，真实 fd 建立约定由
> A/nervud 敲定后接上。Python 侧**不做 Safety 裁决**（决定权/锁存权威/epoch/re-arm 都在 nervud）；
> 本地 `SafetyState` 只是 ServiceHost 复核用的镜像。

## 测试

需要 protobuf **运行时**。本机（Windows）无 AF_UNIX，socket 冒烟测试在 **WSL/Linux** 跑。

```sh
# WSL debian，纯 Python protobuf 运行时经 PYTHONPATH 提供（见开发笔记）
PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION=python \
PYTHONPATH="<protobuf-runtime>:python/protocol:python/sdk:python/sdk/tests" \
python3 -m pytest python/sdk/tests -q
```

覆盖：golden vectors（Python 侧逐字节等于 committed `.binpb`，与 Go/JVM 一致）、
wire（半包/粘包/零长度/超限/畸形）、错误解码（未知 reason/畸形 detail）、
operation 状态机、Safety、client 冒烟（resolve→call/acquire/subscribe）、
ServiceHost 冒烟（register→Dispatch→DispatchResult + fail-closed）。

## 门禁

- `buf lint` / `buf generate` 净（含 Python），`git diff --exit-code`；
- Python golden vectors 与 Go/JVM 逐字节一致；
- `pytest` 全绿（WSL）。
