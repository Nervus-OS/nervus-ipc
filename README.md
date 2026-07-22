# nervus-ipc

Nervus OS（NSOS）控制面协议的**真源仓库**。`.proto` schema、各语言生成物与 SDK 都在这里。

架构依据：`nervud-内核与系统服务架构决策.md` §10（IPC v1 协议基线）与 §10.10（SDK 边界）。

## 为什么协议不放在 nervud 里

公开协议不能藏在 `nervud/internal/`。nervud 是协议的**一个消费者**，不是协议的所有者——
Go 系统 Service、OEM Provider、Kotlin/Java App 都从同一份 schema 生成类型。
放进内核仓库会让「谁能改协议」和「谁能改内核」变成同一个权限。

依赖方向（§10.10）：

```text
nervud                         → nervus-ipc/go/protocol
Go 系统 Service / OEM Service  → nervus-ipc/go/sdk
Kotlin/Java App 及其 Service   → nervus-ipc/jvm/sdk
```

## 为什么三种语言在同一个仓库

因为 NRCP §22.6 要求：

> 同一轮 Host、SDK、Provider 和固件实现也必须依赖**一份已经冻结的 schema 和 golden vectors**，不能各自猜测。

拆成 `nervus-ipc` / `nervus-ipc-go` / `nervus-ipc-jvm` 三个仓库再用 submodule 串起来，
「Go 从哪个 schema commit 生成」和「JVM 从哪个 schema commit 生成」就变成两个
**可以各自漂移的事实**，且没有任何机制强制它们指向同一个 commit。漂移后的表现极其阴险：
两边都能编译、都能跑，只在某个字段的行为上不一致。

单仓库里这件事是结构上成立的——一次 `buf generate` 同时产出三种语言，
一个 commit 里躺着 schema 和三份产物，CI 一条 `git diff --exit-code` 同时覆盖全部。
§10.12 要求的 Go ↔ JVM golden vectors 也才有归宿（放在拆分方案的哪个仓库都不对）。

将来若确有必要拆分，`git subtree split` 可以带着历史把 `go/` 或 `jvm/` 切出去——
**这个方向是可逆的**，反过来（先拆再合）要难得多。

## 布局

```text
nervus-ipc/
├── buf.yaml                    lint + breaking 规则
├── buf.gen.yaml                一次生成 Go / Java / Kotlin 三份产物
├── proto/                      ← 真源，仓库的主体
│   └── nervus/ipc/v1/
│       ├── envelope.proto      Envelope 与全部 body（§10.4）
│       └── status.proto        StatusCode、Success/Failure（§10.12）
├── go/
│   ├── go.mod                  module github.com/nervus-os/nervus-ipc/go
│   ├── protocol/ipcv1/         生成的 Go 类型（提交进仓库，见下）
│   └── sdk/                    Go Client / ServiceHost（手写，待落地）
└── jvm/
    ├── settings.gradle.kts
    ├── gradle/libs.versions.toml
    ├── protocol/
    │   ├── build.gradle.kts
    │   └── src/main/{java,kotlin}/   生成的 Java / Kotlin 类型（提交进仓库）
    └── sdk/                    Kotlin Client / ServiceHost（手写，待落地）
```

根目录不属于任何一种语言——`go.mod` 在 `go/` 下，Gradle 在 `jvm/` 下，
谁也不占据仓库根。

### Go 模块在子目录的后果

`go.mod` 位于 `go/`，模块路径是 `github.com/nervus-os/nervus-ipc/go`，
因此 import 路径是 `github.com/nervus-os/nervus-ipc/go/protocol/ipcv1`。

**git tag 必须带子目录前缀**：

```text
✅ go/v0.1.0
❌ v0.1.0        ← go get 只会报「找不到版本」，不会提示你 tag 写错了
```

## 生成

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
buf lint
buf generate
```

- **Go** 只需要 `protoc-gen-go`，版本由 `go/go.mod` 的 `tool` 指令钉死，
  上面 `go install` 的版本号必须与之一致。
- **Java / Kotlin** 走 protoc 的**内置**生成器（`protoc_builtin`），
  因此额外要求 PATH 上有 `protoc` 本体。两个都要生成：`protobuf-kotlin`
  产出的是 DSL builder，**建立在** `protobuf-java` 的消息类之上，不是替代品。
- `protobuf-java` 运行时版本必须 **>=** 生成所用 protoc 的版本
  （protoc v27 起生成代码带运行时版本自检，低了会在类加载时抛异常）。
  版本在 `jvm/gradle/libs.versions.toml` 中集中管理。

## 生成物提交进仓库

`go/protocol/**/*.pb.go` 与 `jvm/protocol/src/main/**` **提交进 git**，不在构建时生成。
三个理由：

1. **消费者不需要工具链。** nervud、CI、开发机、车上的 RK3588 都只需要 `go build`
   或 `gradle build`。反过来做的话，go.mod 表达不了「先生成再编译」，
   `go build ./...` 就不再自足；Gradle 侧则要求每台构建机都装对版本的 protoc。
2. **让「可重复构建」变成可验证的。** §10.12 要求固定 protoc / runtime / 插件版本。
   只有提交了产物，CI 才能重新生成一次然后 `git diff --exit-code`——不一致就说明
   有人的工具链漂了。不提交的话这句话永远只是口号。
3. **评审的字节就是编译进二进制的字节。** 对 TCB 依赖，这一条本身就够了。

代价是仓库噪音和偶发的合并冲突。对内核依赖来说这个交换划算。

## 兼容规则（§10.12）

- 已发布字段号**永不**改变含义、类型或复用；删除时把号与名写入 `reserved`，不补空档。
- `method_id`、`event_id` 发布后保持稳定，删除后同样保留不用。
- 新增字段必须有安全默认语义，旧接收方可以忽略未知字段。
- 所有枚举以 `*_UNSPECIFIED = 0` 起始；`OK` / `ACCEPTED` 使用非零编号，
  未指定或未知值一律 fail closed。
- 重大不兼容提升 `protocol_major`，并拒绝无法协商的连接。

`buf breaking` 承接前两条，但**在 schema 冻结前不要在 CI 里开启**——
NRCP §22.6 明确允许 Rewrite v1 阶段做破坏性调整，提前开只会逼人养成 `--force` 绕过的习惯。

## 命名空间

| 用途 | 取值 | 说明 |
|---|---|---|
| proto package | `nervus.ipc.v1` | |
| Go module | `github.com/nervus-os/nervus-ipc/go` | |
| Java package | `io.github.nervusos.ipc.v1` | **连字符在 Java 包名里非法**（JLS §3.8），组织名 `nervus-os` 反写后必须去掉 |
| Maven groupId | `io.github.nervus-os` | groupId 是 Maven 坐标不是 Java 包名，**允许**连字符，可与组织名完全一致 |

## 验证记录

在 WSL Debian 上完整跑通（2026-07）。工具链版本：

| 工具 | 版本 | 备注 |
|---|---|---|
| buf | 1.72.0 | 预编译二进制 |
| protoc | 29.3 | 与 protobuf-java 4.29.3 对齐 |
| protoc-gen-go | v1.36.11 | 与 `go/go.mod` 的 `tool` 指令一致 |
| Go | 1.26.0 | 模块声明下限 1.24.0 |
| Gradle | 9.0.0 | 由 wrapper 钉死，含发行包 sha256 |
| JDK（宿主） | 21.0.5-tem | 构建工具链由 foojay 自动 provision 17 |

已验证通过：

- [x] `buf lint`（STANDARD 规则集）与 `buf build` —— 两个 `.proto` 真正编译过
- [x] `buf generate` 产出 2 个 `.pb.go` + 84 个 `.java` + 39 个 `.kt`
- [x] **生成确定性**：连续两次 `buf generate` 产物 sha256 完全一致
      —— 这是「CI 重新生成后 `git diff --exit-code`」这条门禁能成立的前提
- [x] Go 侧 `go mod tidy && go build ./... && go vet ./...` 全绿
- [x] JVM 侧 `./gradlew build` 成功，产物 class major version = 61（确认为 Java 17）
- [x] `.gitignore` 有效，`.gradle/` 与 `build/` 未泄漏进 git status

验证过程中修掉的三个真实缺陷（都不是理论风险）：

1. **`clean: true` 配合 `out: go` 会删掉 `go/go.mod` 和 `go/go.sum`。**
   buf 的 clean 是整目录删除，`out` 必须指向只含生成物的目录。
   已把 Go 的 out 收窄到 `go/protocol`，`module=` 同步加长一段，落盘位置不变。
2. **`jvmToolchain(17)` 在只有 JDK 21 的机器上直接失败**，且默认没有配置
   工具链下载源。已在 `settings.gradle.kts` 加 foojay 解析器。
3. **未声明 `repositories`**，依赖一个也解析不了。已在 `settings.gradle.kts` 用
   `dependencyResolutionManagement` 集中声明，并设 `FAIL_ON_PROJECT_REPOS`
   禁止子模块私自添加依赖源。

### 待办

- [ ] `go/sdk`、`jvm/sdk` 尚未落地（§10.10：Client + ServiceHost）
- [ ] Go ↔ JVM golden vectors（§10.12 结尾）
- [ ] CI：`buf generate && git diff --exit-code` 的可复现性门禁

**按 NRCP §25.2 属于「实现前仍需单独冻结」的内容**，本仓库目前只放了必然成立的部分：

- `ResourceSelector` 的最终语法、公开/私有 label 目录、`REQUIRE_UNIQUE` /
  `SYSTEM_PREFERRED` 枚举与多候选选择 Policy（当前只固定 `type` / `role`）
- `BaseMotion@1-experimental` 的最终字段与 `method_id`
- `Operation` 的 origin binding、typed progress / terminal result 的最终 schema（`[v2+]`）

## 测试要求（§10.12 结尾）

至少覆盖：半包、粘包、零长度、128 KiB 边界、超限长度、畸形 varint、深层/重复字段、
重复 request ID、乱序响应、断线、慢读写、权限撤销、Service 重启、
outcome/code/detail 合法组合、未知 reason、畸形 Service detail，
以及 Go ↔ JVM 的 golden vectors。
