# CleanC2

轻量服务器批量管理系统。面向合法运维场景。

## 能力

- Agent 主动连接 Server
- 心跳与在线状态管理
- 单机 / 批量命令下发
- 按 `agent_ids`、`group_ids`、`tags` 选目标
- 分组模型：`groups` + `group_members`
- 任务取消
- 任务超时回收（服务端兜底标记 `timeout` / `canceled`）
- 命令执行进程组隔离（取消/超时终止整棵进程树）
- SQLite 持久化
- 离线任务重连补发
- 文件上传 / 下载
- 文件传输断点续传（失败后保留 `.part` 临时文件，重试自动续传）
- 传输超时回收（进行中的传输 10 分钟无进展自动标记失败，保留 `.part`）
- 文件传输分块完整性校验（按 `Seq` 重排、缺包/重复检测）
- 文件传输 SHA256 校验
- 文件传输审计
- Agent 基础监控上报 + 指标历史（每 Agent 保留最近 1000 条）
- 本地插件钩子
- 内置 Web Dashboard
- TLS 1.3 / mTLS 参数入口
- 跨站 WebSocket 防护（Origin 校验）
- 恒定时间 Token 比较

## 架构

```mermaid
flowchart LR
    operator["运维人员"]
    browser["浏览器 / Dashboard"]

    subgraph server["Server"]
        api["HTTP API / Dashboard"]
        hub["WebSocket Hub"]
        dispatch["任务分发 / 目标选择"]
        transfer["文件传输管理"]
        plugins["插件管理"]
        store["Store"]
    end

    subgraph agent_side["Agent"]
        conn["长连接客户端"]
        exec["命令执行器"]
        metrics["心跳 / 指标上报"]
        fileio["文件上传 / 下载"]
    end

    db[("SQLite")]
    hook["本地插件"]

    operator --> browser
    browser --> api
    api --> dispatch
    api --> transfer
    api --> store
    api --> plugins
    dispatch --> hub
    transfer --> hub
    hub <--> conn
    conn --> exec
    conn --> metrics
    conn --> fileio
    hub --> store
    store <--> db
    plugins --> hook
    store --> api
```

- 运维人员通过 Web Dashboard 或 API 操作 Server。
- Server 负责鉴权、Agent 长连接管理、任务分发、文件传输、指标聚合和插件触发。
- Agent 主动连回 Server，执行命令，回传结果，并周期上报心跳和基础监控。
- SQLite 持久化 Agent、任务、分组、指标和传输审计，支持离线任务补发。

## 构建和运行

要求 Go 1.26+（`go.mod` 声明）。

先构建：

```bash
mkdir -p ./bin
go build -o ./bin/server ./cmd/server
go build -o ./bin/agent ./cmd/agent
```

测试：

```bash
go test ./...
```

Server:

```bash
./bin/server -config ./config.yaml
```

命令行参数会覆盖 `config.yaml`。

Agent:

```bash
./bin/agent -server ws://127.0.0.1:8080/ws/agent -token cleanc2-dev-token
```

## Web

- Server 侧操作走 Web Dashboard：`/dashboard`
- API 和 Dashboard 都需要 token
- 生成 token：

```bash
openssl rand -hex 32
```

## API

- `GET /healthz`
- `GET /`
- `GET /dashboard`
- `GET /api/v1/agents`
- `GET /api/v1/agents/:id/metrics`
- `GET /api/v1/agents/:id/metrics/history`
- `GET /api/v1/metrics/overview`
- `GET /api/v1/groups`
- `GET /api/v1/groups/:id`
- `POST /api/v1/groups`
- `GET /api/v1/tasks`
- `POST /api/v1/tasks`
- `POST /api/v1/tasks/batch`
- `GET /api/v1/tasks/:id`
- `POST /api/v1/tasks/:id/cancel`
- `POST /api/v1/files/upload`
- `POST /api/v1/files/download`
- `GET /api/v1/transfers`
- `GET /api/v1/transfers/:id`
- `GET /api/v1/plugins`
- `GET /ws/agent`

## 参数

Server:

- `-config`
- `-listen`
- `-token`
- `-api-token`
- `-db`
- `-plugins`
- `-tls-cert`
- `-tls-key`
- `-client-ca`
- `-require-tls`
- `-write-wait`
- `-pong-wait`
- `-ping-period`

Agent:

- `-server`
- `-token`
- `-agent-id`
- `-tags`
- `-heartbeat`
- `-max-backoff`
- `-server-name`
- `-ca-cert`
- `-client-cert`
- `-client-key`

## 安全

- WebSocket 握手只放行无 `Origin` 的客户端（如 Agent 这类非浏览器客户端）以及同源请求，拒绝跨站 WebSocket 劫持。
- Token 校验使用恒定时间比较，避免时序侧信道。
- 文件传输按 `Seq` 重排并校验缺包/重复，配合 SHA256 兜底校验完整性。
- 生产环境务必启用 TLS：Server 配置 `tls_cert` / `tls_key`（可选 `client_ca` 开启 mTLS），Agent 使用 `wss://` 并配置 `-ca-cert` / `-client-cert` / `-client-key`。
- 未启用 TLS 时 Server 与 Agent 都会打印警告；Server 可用 `-require-tls` 强制拒绝在无 TLS 配置时启动。

## 任务执行语义

- 任务在**成功发送给 Agent 时**即标记为 `dispatched`（至多执行一次语义），不再等待 Agent 的 `task_ack`，从而避免「ack 丢失 → 任务仍为 queued → 重连后重复执行」。
- 已下发但长时间未回结果的任务（`dispatched` / `cancel_requested`）由 Server 侧回收器按 `timeout_secs + 30s` 兜底标记为 `timeout` / `canceled`，不会静默卡死。
- Agent 执行命令时使用独立进程组，取消或超时会终止整棵进程树，避免后台子进程残留。

## 插件

`server` 会加载 `-plugins` 目录下的可执行文件。

hook:

- `agent_connected`
- `task_result`
- `transfer_done`
- `metrics_report`

事件 JSON 从 stdin 传入。样例见 `plugins/README.md` 和 `plugins/example-plugin.sh.sample`。

## 限制

- 只支持 Shell 命令执行（`/bin/sh -c`，无 Windows 原生支持）
- 监控为基础指标（无 CPU/内存占用率、进程级指标），历史按 Agent 保留最近 1000 条
- 插件只支持本地可执行文件钩子
- 文件传输失败后 `.part` 临时文件会保留用于续传（同一目标路径的传输视为同一任务）
