# Plugins

`server` 会从 `-plugins` 指定目录加载可执行文件插件。

调用方式：

- 启动参数默认是 `-plugins plugins`
- 每个可执行文件会在事件发生时被调用一次
- 第一个参数是 hook 名称
- 事件 JSON 会通过 stdin 传入

当前 hook：

- `agent_connected`
- `task_result`
- `transfer_done`
- `metrics_report`

可参考同目录的 `example-plugin.sh.sample`。
