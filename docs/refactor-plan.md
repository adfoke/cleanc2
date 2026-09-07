# CleanC2 改造方案 v2 —— CLI-first + Protobuf

> 状态：**v1 已拍板（2026-09-08）**。三项决定：① Protobuf 走 A1 全量替换；② 控制面走 B2（Unix socket 为默认，`-listen` 保留 TCP 逃生门）；③ CLI 为独立二进制，不与 server 合并成单二进制双角色。

## 0. 一个要先分清的概念

「Server 端的 web」其实是两层东西：

1. **Dashboard（HTML 页面）** — `internal/server/dashboard.go`，731 行内嵌 HTML/CSS/JS，`/` 和 `/dashboard` 两个路由。**这一层没有任何保留理由，直接删。**
2. **HTTP JSON API（控制面）** — `/api/v1/*` + `/ws/agent`。**这一层删不掉**：Agent 的 WebSocket 长连接挂在 server 进程里，任务必须由这个进程分发。CLI 若绕过它直连 SQLite，只能读死数据、没法给在线 Agent 派活，还会和 server 抢写锁。

所以「取消 web」的准确含义是：**删掉浏览器层，HTTP API 降级为 CLI 的私有后端**。下面 §3 会讨论连 HTTP 都不要的激进方案。

## 1. Go 升级到最新

- `go.mod`：`go 1.26` → `go 1.27`（当前官方最新 go1.27.1，本机已装）。
- 跑 `go fix` 新 fixer：`atomictypes` / `embedlit` / `slicesbackward` / `unsafefuncs` / `waitgroupgo`。
- 按 `use-modern-go` 准则（54 条适用）清理一轮：`slices`/`maps` 包替代手写循环、`for range n`、`min/max` 内建等。纯机械改动，测试全绿即可。
- 无风险，单独一个 commit，放在最前面做，避免和后面的大改动混在一起。

## 2. CLI 设计（核心交付物）

### 2.1 形态

新增 `cmd/cli`，产物 `./bin/cleanc2`。单二进制双角色：`cleanc2 serve` 启动服务端，其余子命令是操控端。Agent 保持独立二进制（要分发到被控机，不能拖 CLI 的依赖）。

### 2.2 AI 友好的硬性要求

| 要求 | 做法 |
|---|---|
| 输出可解析 | 默认 JSON（紧凑单对象）；批量列表走 NDJSON 到 stdout；人类格式只在显式 `--pretty` 时给 |
| 错误可判定 | 稳定退出码：`0` 成功 / `1` 任务失败 / `2` 连接失败 / `3` 鉴权失败 / `4` 参数错误；错误也输出 JSON（`{"error":{"code","message"}}`），永远不往 stdout 掺日志 |
| 无交互 | 任何子命令不追问、无 TTY 检测、无进度条 spinner；长任务用 `--wait` 阻塞到终态一次性返回 |
| 可自描述 | `cleanc2 schema` 输出全部命令+参数的 JSON 规格，AI 拉一次就能学会整套 CLI（等价于给 agent 的 man page） |
| 配置走环境 | `CLEANC2_SERVER` / `CLEANC2_TOKEN` 环境变量兜底，flag 可覆盖；token 不出现在命令行回显里 |
| 幂等与安全 | 危险操作（批量下发、删除）要求显式 `--yes`；任务创建返回 `task_id`，重复 cancel 不报错 |

### 2.3 命令面（v1 范围）

```
cleanc2 serve            -config ...              # 原 server
cleanc2 health                                     # 连通性+鉴权自检
cleanc2 agents list [--filter tag=x] / get <id> / history <id>
cleanc2 groups list / create / add / remove
cleanc2 run --cmd "uptime" --agents id1,id2 | --group g1 | --tag k=v
          [--timeout 30] [--wait]                  # 创建任务；--wait 阻塞收结果
cleanc2 tasks list / get <id> / cancel <id>        # get --wait 同样可阻塞
cleanc2 push --agent <id> --local f --remote p [--wait]
cleanc2 pull --agent <id> --remote p --local f [--wait]
cleanc2 transfers list / get <id>
cleanc2 metrics get <id> / history <id> / overview
cleanc2 schema                                     # 机器可读命令规格
```

`run --wait` 的结果聚合格式（这是 AI 运维闭环的关键输出）：

```json
{"task_id":"t1","status":"done","results":[
  {"agent_id":"a1","exit_code":0,"stdout":"...","stderr":"...","duration_ms":12}]}
```

实现层就是个薄 HTTP client + 轮询（`--wait` 默认 500ms 起指数退避、2s 封顶），不引入 WebSocket 客户端复杂度。CLI 内部代码全部放 `internal/cli/`，与 server 解耦（只依赖 API 契约）。

### 2.4 顺手产物

给 Alma/AI 生态写一个 `SKILL.md`（`docs/ai-usage.md` 或直接做成 skill）：命令速查 + 退出码语义 + 「先 `schema` 后用」的约定，让任何 LLM agent 拿起来就能操作。

## 3. 讨论点 A：Protobuf 模块

### 现状问题

- `Envelope{type, payload}` + 每消息一个 JSON struct，契约全靠约定，两端字段漂移没有编译期防线。
- `FileTransferChunk.Data` 是 JSON string——二进制被 base64 编码，膨胀 33%，还强制 string 校验开销。文件传输是现在线协议里最大流量。
- 类型定义散在 `internal/protocol/types.go`（140 行），无版本化字段语义。

### 三个方案

| | A1 全量替换：WS 二进制帧直接跑 protobuf | A2 信封混合：Envelope 保留 JSON，payload 换 protobuf 字节（base64→binary 帧） | A3 换 gRPC 双向流 |
|---|---|---|---|
| 描述 | `proto/` 目录定义全部消息，`protoc` 生成 Go 类型，server/agent 两端共用 | 同上 .proto，仅替换 payload 编解码，`type` 路由字段不动 | 弃 WS，agent 拨 server 的 bidirectional stream RPC |
| 改动量 | codec + hub 读写循环 + agent client 读写循环，约 300 行 | 只改 `protocol.Marshal/Unmarshal` 两侧，约 100 行 | 引入 grpc-go 全家桶，WS 层推翻，最大 |
| 收益 | 彻底：类型化、体积、性能一次到位 | 拿到类型化+.bytes 字段去 base64 膨胀；信封仍有一层 JSON 开销 | 多路复用/流控/拦截器白送 |
| 风险 | 新旧 agent 混跑需协商 | 双重编码（JSON 套 bytes）略丑 | 依赖重、调试麻烦、对「agent 单条长连接」场景是杀鸡用牛刀 |

**推荐 A1 + 握手协商**：`AgentHello` 加 `proto_version`（或 capabilities 列表），server 按协商结果决定该连接后续帧用 JSON 还是 protobuf；老 agent 不掉线，新 agent 走新协议。理由：

1. 本项目还没真实部署存量，协商逻辑（约 20 行）是为将来留的门，成本低。
2. WS binary frame 天然解决 base64 膨胀，chunk 传输直接受益。
3. gRPC（A3）留作以后的选项，现在 WS 的拨出+TLS 语义够用，不推翻。

### 落点结构

```
proto/cleanc2/v1/wire.proto     # AgentHello/Task/TaskResult/FileTransfer* 等全部消息
internal/protocol/              # 手写层保留：Envelope 路由、协商
internal/protocol/pb/           # protoc 生成码（生成脚本 scripts/gen-proto.sh，Makefile 入口）
```

- 生成码**提交进仓库**（免 CI 装 protoc，`go test ./...` 即可复现）。
- 消息命名与现有 Type* 常量一一对应，`oneof EnvelopePayload` 可选做二期。
- SQLite 存储格式不动（proto 只管线上传输，rest 持久化仍是现状 JSON/列），避免无谓迁移。
- 兼容性测试：对拍 JSON→proto→JSON 的往返，覆盖 chunk 乱序/缺包路径（现有 chunk_test 的夹具直接复用）。

## 4. 讨论点 B：Server 端 web 服务去留

前提：Dashboard HTML（§0 层 1）必删，连带 hub.go 两条路由，`/` 改为 404 或最小 JSON 索引。WS Origin 校验收紧为「仅无 Origin 客户端」——没有浏览器了，同源逻辑可整个去掉。

控制面剩下三个选项：

1. **B1 保留 HTTP JSON API（推荐）**：CLI 走 `http://host:port/api/v1`。改动最小，API 已有全套实现和 token 鉴权；远程操控天然可用；curl 可直接调试。
2. **B2 收紧为 Unix Socket + HTTP over UDS**：删 TCP 监听，server 只开 `cleanc2.sock`。攻击面最小（无网络暴露，token 都可以省），适合「CLI 和 server 必然同机」的运维模型。保留 `-listen` flag 时退回 TCP（向后兼容远程场景）。
3. **B3 彻底去网络层**：CLI 直接读 SQLite + 通过信号/命令队列喂 server。**否决**——写路径（派任务给在线 agent）绕不开 server 进程，等于要把整个 hub 搬进 CLI，自废架构。

**决定：B2 为默认（2026-09-08 拍板），实现为「双监听面」**——原 B2 提案有个洞：Agent 在远程机器上，`/ws/agent` 必须留在 TCP，否则所有 agent 掉线。修正后的拓扑：

- **Agent 面**：`-listen`（默认 `:8080`）只挂 `/ws/agent` + `/healthz`，鉴权走 hello token（不变）。
- **Operator 面**：CLI/API 专用。默认只监听 Unix socket（`-operator-uds`，默认 `./cleanc2.sock`，文件权限 `0600`，**免 token**——文件系统权限即边界，且消灭「token 躺在 config.yaml」的常态泄漏）。
- **逃生门**：`-operator-listen <addr>` 显式指定才把 operator 面再挂一份 TCP，此时 token 鉴权强制（Bearer/Basic/X-Auth-Token 三式保留）。UDS 与 TCP 共用一个 gin engine，鉴权按配置整体启停。
- `/` 与 `/dashboard` 随 Dashboard 一起删除；WS `CheckOrigin` 收紧为**拒绝任何携带 Origin 的握手**（浏览器 fetch 可带 `Origin: null` 打无 Origin 检查的端点，全拒才是正确姿势）。
- CLI 地址解析：值以 `/`、`~`、`.` 开头或无 scheme 无端口 → 按 UDS 路径；`http(s)://` 前缀 → TCP。

## 5. 实施顺序（Sprint 切分）

| # | 内容 | 验收 |
|---|---|---|
| S0 | Go 1.27 + go fix + modernization | build/vet/test 全绿，独立 commit |
| S1 | 删 dashboard + 路由清理 + WS Origin 收紧 + README/API 文档同步 | `go test ./...` 绿；`/dashboard` 返回 404 的回归测试 |
| S2 | ✅ 已完成 | proto/ + pb 生成码入库；双栈按 opcode 自描述，hello 协商；对拍/协商 e2e 齐；突变五连（丢字段、截数据、三处翻转逻辑）全命中 |
| S3 | CLI 骨架（serve 搬迁 + health + agents/groups/tasks 只读命令 + schema） | 每命令 JSON 输出快照测试；退出码测试 |
| S4 | run --wait / push / pull 写路径 + 危险操作 --yes | 起一个进程内嵌 server 的端到端测试（现有 hub_test 模式可复用） |
| S5 | `docs/ai-usage.md` + 收尾 | 拿真实 LLM agent 用 `schema` 输出现场跑一轮 `run --wait` 冒烟 |

依赖关系：S0、S1 可并行；S2 独立于 S3；S3 依赖现有 API 形状冻结（S1 完成后）。

## 6. 明确不做（减法）

- 不做 gRPC、不做 GraphQL、不做 Web UI 的任何替代前端（连静态页都不留）。
- 不改 Agent 的拨出模型和 SQLite 存储格式。
- 不做多用户/RBAC（token 单租户语义维持现状）。
- CLI 不做交互式 REPL（AI 不需要，人类可以用 shell history）。
�。
