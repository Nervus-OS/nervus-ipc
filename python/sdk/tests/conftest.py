"""让测试无需安装即可导入 SDK 与生成的 protobuf。

- 把 `python/sdk`（含 `nervus_ipc` 包）放上 sys.path；
- `nervus_ipc.__init__` 会自动把兄弟 `python/protocol`（生成物）挂上；
- protobuf **运行时**（google.protobuf）与纯 Python 实现由运行命令的
  PYTHONPATH / PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION 提供（见 README）。
"""

import os
import sys

_TESTS = os.path.dirname(os.path.abspath(__file__))
_SDK = os.path.dirname(_TESTS)  # python/sdk
_PYTHON = os.path.dirname(_SDK)  # python
_PROTOCOL = os.path.join(_PYTHON, "protocol")  # 生成的 protobuf 源根
for p in (_SDK, _TESTS, _PROTOCOL):
    if p not in sys.path:
        sys.path.insert(0, p)
