package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Service) handleDashboard(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(dashboardHTML))
}

const dashboardHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>CleanC2 Dashboard</title>
  <style>
    :root {
      --bg: #f3efe6;
      --panel: rgba(255,255,255,0.9);
      --ink: #171717;
      --muted: #66604f;
      --line: rgba(23,23,23,0.08);
      --accent: #0f766e;
      --accent-2: #c2410c;
      --ok: #166534;
      --warn: #b45309;
      --bad: #b91c1c;
      --shadow: 0 18px 40px rgba(38, 31, 20, 0.12);
      --radius: 20px;
      --radius-sm: 12px;
      --mono: "IBM Plex Mono", "SF Mono", "Menlo", monospace;
      --sans: "IBM Plex Sans", "Avenir Next", "Segoe UI", sans-serif;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: var(--sans);
      color: var(--ink);
      background:
        radial-gradient(circle at top left, rgba(15,118,110,0.18), transparent 28%),
        radial-gradient(circle at top right, rgba(194,65,12,0.16), transparent 24%),
        linear-gradient(180deg, #f7f4ec 0%, #efe7d8 100%);
      min-height: 100vh;
    }
    .shell {
      max-width: 1480px;
      margin: 0 auto;
      padding: 28px 20px 40px;
    }
    .hero {
      display: grid;
      gap: 18px;
      margin-bottom: 20px;
    }
    .hero-card {
      background: linear-gradient(135deg, rgba(255,255,255,0.94), rgba(255,248,238,0.88));
      border: 1px solid rgba(255,255,255,0.7);
      border-radius: 28px;
      box-shadow: var(--shadow);
      padding: 24px;
    }
    .title {
      display: flex;
      align-items: flex-end;
      justify-content: space-between;
      gap: 12px;
      flex-wrap: wrap;
    }
    h1 {
      margin: 0;
      font-size: clamp(32px, 6vw, 62px);
      line-height: 0.95;
      letter-spacing: -0.06em;
    }
    .subtitle {
      color: var(--muted);
      max-width: 760px;
      font-size: 15px;
    }
    .pillbar {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      margin-top: 18px;
    }
    .pill {
      padding: 10px 14px;
      border-radius: 999px;
      background: rgba(23,23,23,0.04);
      color: var(--muted);
      font-size: 13px;
      border: 1px solid rgba(23,23,23,0.06);
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(12, minmax(0, 1fr));
      gap: 16px;
    }
    .panel {
      grid-column: span 12;
      background: var(--panel);
      border: 1px solid rgba(255,255,255,0.72);
      border-radius: var(--radius);
      box-shadow: var(--shadow);
      overflow: hidden;
    }
    .panel-head {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
      padding: 16px 18px 0;
    }
    .panel h2 {
      margin: 0;
      font-size: 16px;
      letter-spacing: 0.02em;
      text-transform: uppercase;
    }
    .panel-body { padding: 18px; }
    .stats {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
    }
    .stat {
      padding: 16px;
      border-radius: 16px;
      background: linear-gradient(180deg, rgba(255,255,255,0.76), rgba(245,241,233,0.9));
      border: 1px solid var(--line);
      min-height: 110px;
    }
    .stat .label {
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.06em;
    }
    .stat .value {
      margin-top: 14px;
      font-size: 34px;
      font-weight: 700;
      letter-spacing: -0.05em;
    }
    .stat .meta {
      margin-top: 6px;
      color: var(--muted);
      font-size: 13px;
    }
    .col-4 { grid-column: span 4; }
    .col-5 { grid-column: span 5; }
    .col-6 { grid-column: span 6; }
    .col-7 { grid-column: span 7; }
    .col-8 { grid-column: span 8; }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 14px;
    }
    th, td {
      text-align: left;
      padding: 11px 10px;
      border-bottom: 1px solid var(--line);
      vertical-align: top;
    }
    th { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: 0.06em; }
    tr:hover td { background: rgba(15,118,110,0.03); }
    .clickable { cursor: pointer; }
    .kvs {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .kv {
      padding: 12px 14px;
      border-radius: 14px;
      background: rgba(23,23,23,0.035);
      border: 1px solid var(--line);
    }
    .kv .k { color: var(--muted); font-size: 12px; text-transform: uppercase; }
    .kv .v { margin-top: 6px; font-family: var(--mono); font-size: 13px; overflow-wrap: anywhere; }
    .forms {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 14px;
    }
    .form-box {
      padding: 14px;
      border-radius: 16px;
      background: rgba(23,23,23,0.03);
      border: 1px solid var(--line);
    }
    .form-box h3 {
      margin: 0 0 12px;
      font-size: 14px;
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    label {
      display: block;
      margin: 0 0 10px;
      font-size: 12px;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    input, textarea {
      width: 100%;
      margin-top: 6px;
      border: 1px solid rgba(23,23,23,0.12);
      background: rgba(255,255,255,0.9);
      border-radius: 12px;
      padding: 11px 12px;
      font: inherit;
      color: var(--ink);
    }
    textarea { min-height: 88px; resize: vertical; }
    button {
      appearance: none;
      border: none;
      background: linear-gradient(135deg, var(--accent), #115e59);
      color: white;
      padding: 12px 16px;
      border-radius: 12px;
      font: inherit;
      font-weight: 700;
      cursor: pointer;
    }
    button.secondary {
      background: linear-gradient(135deg, var(--accent-2), #9a3412);
    }
    button.ghost {
      background: rgba(23,23,23,0.07);
      color: var(--ink);
      padding: 8px 10px;
      font-size: 12px;
    }
    .hint, .statusline {
      color: var(--muted);
      font-size: 13px;
    }
    .badge {
      display: inline-block;
      padding: 5px 8px;
      border-radius: 999px;
      font-size: 12px;
      font-weight: 700;
      background: rgba(23,23,23,0.06);
    }
    .status-success { color: var(--ok); }
    .status-online { color: var(--ok); }
    .status-failed, .status-canceled { color: var(--bad); }
    .status-timeout, .status-cancel_requested { color: var(--warn); }
    .mono { font-family: var(--mono); }
    .truncate { max-width: 360px; overflow-wrap: anywhere; }
    pre {
      margin: 0;
      padding: 12px;
      border-radius: 12px;
      background: rgba(23,23,23,0.045);
      border: 1px solid var(--line);
      font-family: var(--mono);
      font-size: 12px;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    @media (max-width: 1100px) {
      .col-4, .col-5, .col-6, .col-7, .col-8 { grid-column: span 12; }
      .stats, .forms, .kvs { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <div class="shell">
    <section class="hero">
      <div class="hero-card">
        <div class="title">
          <div>
            <h1>CleanC2</h1>
            <div class="subtitle">一个内置页面。看状态，发命令，建分组，传文件，不用另起前端工程。</div>
          </div>
          <div class="statusline" id="refreshState">加载中</div>
        </div>
        <div class="pillbar">
          <div class="pill">Dashboard</div>
          <div class="pill">Realtime Snapshot</div>
          <div class="pill">No Build Step</div>
        </div>
      </div>
    </section>

    <section class="grid">
      <div class="panel col-12">
        <div class="panel-body">
          <div class="stats">
            <div class="stat"><div class="label">Agents</div><div class="value" id="statAgents">0</div><div class="meta" id="statAgentsMeta">0 online</div></div>
            <div class="stat"><div class="label">Pending</div><div class="value" id="statPending">0</div><div class="meta">queued tasks</div></div>
            <div class="stat"><div class="label">Transfers</div><div class="value" id="statTransfers">0</div><div class="meta">active transfers</div></div>
            <div class="stat"><div class="label">Plugins</div><div class="value" id="statPlugins">0</div><div class="meta">loaded executables</div></div>
          </div>
        </div>
      </div>

      <div class="panel col-7">
        <div class="panel-head"><h2>Agents</h2><div class="hint">点一行看指标</div></div>
        <div class="panel-body"><div id="agentsWrap"></div></div>
      </div>

      <div class="panel col-5">
        <div class="panel-head"><h2>Metrics</h2><div class="hint" id="metricsHint">未选择 Agent</div></div>
        <div class="panel-body"><div class="kvs" id="metricsWrap"></div></div>
      </div>

      <div class="panel col-6">
        <div class="panel-head"><h2>Actions</h2><div class="hint">常用操作</div></div>
        <div class="panel-body">
          <div class="forms">
            <form class="form-box" id="taskForm">
              <h3>Single Task</h3>
              <label>Agent ID<input name="agent_id" required></label>
              <label>Command<textarea name="command" required></textarea></label>
              <label>Timeout Secs<input name="timeout_secs" value="60"></label>
              <button type="submit">Run</button>
            </form>

            <form class="form-box" id="batchForm">
              <h3>Batch Task</h3>
              <label>Agent IDs<input name="agent_ids" placeholder="a,b"></label>
              <label>Group IDs<input name="group_ids" placeholder="group-a,group-b"></label>
              <label>Tags<input name="tags" placeholder="prod,edge"></label>
              <label>Command<textarea name="command" required></textarea></label>
              <button type="submit">Dispatch</button>
            </form>

            <form class="form-box" id="groupForm">
              <h3>Create Group</h3>
              <label>Group ID<input name="id" placeholder="optional"></label>
              <label>Name<input name="name" required></label>
              <label>Agent IDs<input name="agent_ids" placeholder="a,b"></label>
              <button type="submit">Save</button>
            </form>

            <form class="form-box" id="uploadForm">
              <h3>Upload File</h3>
              <label>Agent ID<input name="agent_id" required></label>
              <label>Local Path<input name="local_path" required></label>
              <label>Remote Path<input name="remote_path" required></label>
              <button type="submit" class="secondary">Upload</button>
            </form>

            <form class="form-box" id="downloadForm">
              <h3>Download File</h3>
              <label>Agent ID<input name="agent_id" required></label>
              <label>Remote Path<input name="remote_path" required></label>
              <label>Local Path<input name="local_path" required></label>
              <button type="submit" class="secondary">Download</button>
            </form>
          </div>
          <div class="statusline" id="actionState">就绪</div>
        </div>
      </div>

      <div class="panel col-6">
        <div class="panel-head"><h2>Groups</h2><div class="hint">静态成员表</div></div>
        <div class="panel-body"><div id="groupsWrap"></div></div>
      </div>

      <div class="panel col-8">
        <div class="panel-head"><h2>Recent Tasks</h2><div class="hint">最近 20 条</div></div>
        <div class="panel-body"><div id="tasksWrap"></div></div>
      </div>

      <div class="panel col-4">
        <div class="panel-head"><h2>Task Detail</h2><div class="hint" id="taskDetailHint">未选择任务</div></div>
        <div class="panel-body"><div class="kvs" id="taskDetailWrap"></div></div>
      </div>

      <div class="panel col-4">
        <div class="panel-head"><h2>Plugins</h2><div class="hint">已加载</div></div>
        <div class="panel-body"><div id="pluginsWrap"></div></div>
      </div>

      <div class="panel col-8">
        <div class="panel-head"><h2>Recent Transfers</h2><div class="hint">最近 20 条</div></div>
        <div class="panel-body"><div id="transfersWrap"></div></div>
      </div>

      <div class="panel col-4">
        <div class="panel-head"><h2>Transfer Detail</h2><div class="hint" id="transferDetailHint">未选择传输</div></div>
        <div class="panel-body"><div class="kvs" id="transferDetailWrap"></div></div>
      </div>
    </section>
  </div>

  <script>
    const state = { selectedAgentId: "", selectedTaskId: "", selectedTransferId: "" };

    function byId(id) { return document.getElementById(id); }
    function esc(v) {
      return String(v ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
    }
    function prettyTime(value) {
      if (!value) return "-";
      const d = new Date(value);
      if (Number.isNaN(d.getTime())) return value;
      return d.toLocaleString();
    }
    function statusClass(v) {
      return "status-" + String(v || "").toLowerCase();
    }
    function splitCSV(value) {
      return String(value || "").split(",").map(v => v.trim()).filter(Boolean);
    }
    async function getJSON(url) {
      const res = await fetch(url);
      if (!res.ok) throw new Error(await res.text());
      return res.json();
    }
    async function sendJSON(url, body) {
      const res = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || res.statusText);
      return data;
    }

    function renderAgents(agents) {
      if (!state.selectedAgentId && agents[0]) state.selectedAgentId = agents[0].agent_id;
      byId("agentsWrap").innerHTML =
        '<table><thead><tr><th>Agent</th><th>Host</th><th>Status</th><th>Tags</th><th>Pending</th><th>Last Seen</th></tr></thead><tbody>' +
        agents.map(agent =>
          '<tr class="clickable" data-agent-id="' + esc(agent.agent_id) + '">' +
            '<td class="mono">' + esc(agent.agent_id) + '</td>' +
            '<td>' + esc(agent.hostname) + '</td>' +
            '<td class="' + (agent.online ? 'status-online' : 'status-failed') + '">' + (agent.online ? 'online' : 'offline') + '</td>' +
            '<td>' + (esc((agent.tags || []).join(', ')) || '-') + '</td>' +
            '<td>' + agent.pending_count + '</td>' +
            '<td>' + prettyTime(agent.last_seen_at) + '</td>' +
          '</tr>'
        ).join("") +
        '</tbody></table>';
      byId("agentsWrap").querySelectorAll("[data-agent-id]").forEach(row => {
        row.addEventListener("click", () => {
          state.selectedAgentId = row.dataset.agentId;
          refreshMetrics();
        });
      });
    }

    function renderGroups(groups) {
      byId("groupsWrap").innerHTML = groups.length ?
        '<table><thead><tr><th>Name</th><th>ID</th><th>Members</th></tr></thead><tbody>' +
        groups.map(group =>
          '<tr><td>' + esc(group.name) + '</td><td class="mono">' + esc(group.id) + '</td><td>' + group.member_count + '</td></tr>'
        ).join("") +
        '</tbody></table>' :
        '<div class="hint">暂无分组</div>';
    }

    function renderTasks(tasks) {
      if (!state.selectedTaskId && tasks[0]) state.selectedTaskId = tasks[0].task.id;
      byId("tasksWrap").innerHTML = tasks.length ?
        '<table><thead><tr><th>Task</th><th>Agent</th><th>Status</th><th>Command</th><th>Created</th></tr></thead><tbody>' +
        tasks.map(item =>
          '<tr class="clickable" data-task-id="' + esc(item.task.id) + '">' +
            '<td class="mono">' + esc(item.task.id) + '</td>' +
            '<td class="mono">' + esc(item.task.agent_id) + '</td>' +
            '<td class="' + statusClass(item.state) + '">' + esc(item.state) +
              (["queued", "dispatched", "cancel_requested"].includes(String(item.state)) ? ' <button class="ghost" data-cancel-task="' + esc(item.task.id) + '">Cancel</button>' : '') +
            '</td>' +
            '<td class="truncate mono">' + esc(item.task.command) + '</td>' +
            '<td>' + prettyTime(item.task.created_at) + '</td>' +
          '</tr>'
        ).join("") +
        '</tbody></table>' :
        '<div class="hint">暂无任务</div>';
      byId("tasksWrap").querySelectorAll("[data-task-id]").forEach(row => {
        row.addEventListener("click", () => {
          state.selectedTaskId = row.dataset.taskId;
          refreshTaskDetail();
        });
      });
      byId("tasksWrap").querySelectorAll("[data-cancel-task]").forEach(button => {
        button.addEventListener("click", async (event) => {
          event.stopPropagation();
          byId("actionState").textContent = "取消中";
          try {
            const data = await sendJSON("/api/v1/tasks/" + encodeURIComponent(button.dataset.cancelTask) + "/cancel", {});
            byId("actionState").textContent = "已请求取消 " + data.task_id;
            state.selectedTaskId = data.task_id;
            refreshAll();
          } catch (err) {
            byId("actionState").textContent = err.message;
          }
        });
      });
    }

    function renderTransfers(transfers) {
      if (!state.selectedTransferId && transfers[0]) state.selectedTransferId = transfers[0].transfer_id;
      byId("transfersWrap").innerHTML = transfers.length ?
        '<table><thead><tr><th>Transfer</th><th>Agent</th><th>Direction</th><th>Status</th><th>Checksum</th><th>Bytes</th></tr></thead><tbody>' +
        transfers.map(item =>
          '<tr class="clickable" data-transfer-id="' + esc(item.transfer_id) + '">' +
            '<td class="mono">' + esc(item.transfer_id) + '</td>' +
            '<td class="mono">' + esc(item.agent_id) + '</td>' +
            '<td>' + esc(item.direction) + '</td>' +
            '<td class="' + statusClass(item.status) + '">' + esc(item.status) + '</td>' +
            '<td>' + (item.checksum_verified ? "<span class='status-success'>verified</span>" : "-") + '</td>' +
            '<td>' + (item.bytes_transferred || 0) + ' / ' + (item.size || 0) + '</td>' +
          '</tr>'
        ).join("") +
        '</tbody></table>' :
        '<div class="hint">暂无传输</div>';
      byId("transfersWrap").querySelectorAll("[data-transfer-id]").forEach(row => {
        row.addEventListener("click", () => {
          state.selectedTransferId = row.dataset.transferId;
          refreshTransferDetail();
        });
      });
    }

    function renderPlugins(plugins) {
      byId("pluginsWrap").innerHTML = plugins.length ?
        plugins.map(plugin =>
          '<div class="kv"><div class="k">' + esc(plugin.name) + '</div><div class="v">' + esc(plugin.path) + '</div></div>'
        ).join("") :
        '<div class="hint">未加载插件</div>';
    }

    async function refreshMetrics() {
      if (!state.selectedAgentId) {
        byId("metricsHint").textContent = "未选择 Agent";
        byId("metricsWrap").innerHTML = "";
        return;
      }
      byId("metricsHint").textContent = state.selectedAgentId;
      try {
        const metrics = await getJSON("/api/v1/agents/" + encodeURIComponent(state.selectedAgentId) + "/metrics");
        byId("metricsWrap").innerHTML = [
          ["Timestamp", prettyTime(metrics.timestamp)],
          ["Uptime", metrics.uptime_secs + "s"],
          ["CPU", metrics.cpu_count],
          ["Goroutines", metrics.goroutines],
          ["Memory", metrics.process_memory_bytes],
          ["Disk Total", metrics.root_disk_total_bytes],
          ["Disk Free", metrics.root_disk_free_bytes],
        ].map(([k, v]) => '<div class="kv"><div class="k">' + esc(k) + '</div><div class="v">' + esc(v) + '</div></div>').join("");
      } catch (err) {
        byId("metricsWrap").innerHTML = '<div class="hint">' + esc(err.message) + '</div>';
      }
    }

    async function refreshTaskDetail() {
      if (!state.selectedTaskId) {
        byId("taskDetailHint").textContent = "未选择任务";
        byId("taskDetailWrap").innerHTML = "";
        return;
      }
      byId("taskDetailHint").textContent = state.selectedTaskId;
      try {
        const item = await getJSON("/api/v1/tasks/" + encodeURIComponent(state.selectedTaskId));
        const result = item.result || {};
        byId("taskDetailWrap").innerHTML = [
          ["Agent", item.task.agent_id],
          ["Status", item.state],
          ["Command", item.task.command],
          ["Created", prettyTime(item.task.created_at)],
          ["Exit", result.exit_code ?? "-"],
          ["Duration", result.duration_ms ? result.duration_ms + "ms" : "-"],
          ["Stdout", result.stdout || "-"],
          ["Stderr", result.stderr || "-"],
        ].map(([k, v]) => '<div class="kv"><div class="k">' + esc(k) + '</div><div class="v">' + esc(v) + '</div></div>').join("");
      } catch (err) {
        byId("taskDetailWrap").innerHTML = '<div class="hint">' + esc(err.message) + '</div>';
      }
    }

    async function refreshTransferDetail() {
      if (!state.selectedTransferId) {
        byId("transferDetailHint").textContent = "未选择传输";
        byId("transferDetailWrap").innerHTML = "";
        return;
      }
      byId("transferDetailHint").textContent = state.selectedTransferId;
      try {
        const item = await getJSON("/api/v1/transfers/" + encodeURIComponent(state.selectedTransferId));
        byId("transferDetailWrap").innerHTML = [
          ["Agent", item.agent_id],
          ["Direction", item.direction],
          ["Status", item.status],
          ["Local", item.local_path || "-"],
          ["Remote", item.remote_path || "-"],
          ["Size", item.size || 0],
          ["Bytes", item.bytes_transferred || 0],
          ["Checksum", item.checksum_sha256 || "-"],
          ["Verified", item.checksum_verified ? "true" : "false"],
          ["Message", item.message || "-"],
        ].map(([k, v]) => '<div class="kv"><div class="k">' + esc(k) + '</div><div class="v">' + esc(v) + '</div></div>').join("");
      } catch (err) {
        byId("transferDetailWrap").innerHTML = '<div class="hint">' + esc(err.message) + '</div>';
      }
    }

    async function refreshAll() {
      byId("refreshState").textContent = "刷新中";
      try {
        const [overview, agents, groups, tasks, transfers, plugins] = await Promise.all([
          getJSON("/api/v1/metrics/overview"),
          getJSON("/api/v1/agents"),
          getJSON("/api/v1/groups"),
          getJSON("/api/v1/tasks?limit=20"),
          getJSON("/api/v1/transfers?limit=20"),
          getJSON("/api/v1/plugins"),
        ]);

        byId("statAgents").textContent = overview.total_agents;
        byId("statAgentsMeta").textContent = overview.online_agents + " online";
        byId("statPending").textContent = overview.pending_tasks;
        byId("statTransfers").textContent = overview.active_transfers;
        byId("statPlugins").textContent = overview.plugins;

        renderAgents(agents);
        renderGroups(groups);
        renderTasks(tasks);
        renderTransfers(transfers);
        renderPlugins(plugins);
        refreshMetrics();
        refreshTaskDetail();
        refreshTransferDetail();
        byId("refreshState").textContent = "已刷新 " + new Date().toLocaleTimeString();
      } catch (err) {
        byId("refreshState").textContent = "刷新失败";
        byId("actionState").textContent = err.message;
      }
    }

    function bindForm(id, builder, successMessage) {
      byId(id).addEventListener("submit", async (event) => {
        event.preventDefault();
        const form = event.currentTarget;
        const body = builder(new FormData(form));
        byId("actionState").textContent = "提交中";
        try {
          const data = await sendJSON(form.dataset.url || form.getAttribute("action") || form.dataset.action || form.action || builder.url, body);
          byId("actionState").textContent = successMessage(data);
          form.reset();
          refreshAll();
        } catch (err) {
          byId("actionState").textContent = err.message;
        }
      });
    }

    bindForm("taskForm", (fd) => ({
      agent_id: fd.get("agent_id"),
      command: fd.get("command"),
      timeout_secs: Number(fd.get("timeout_secs") || 60),
    }), (data) => "已创建任务 " + data.task_id);
    byId("taskForm").dataset.action = "/api/v1/tasks";

    bindForm("batchForm", (fd) => ({
      agent_ids: splitCSV(fd.get("agent_ids")),
      group_ids: splitCSV(fd.get("group_ids")),
      tags: splitCSV(fd.get("tags")),
      command: fd.get("command"),
      timeout_secs: 60,
    }), (data) => "已批量创建 " + data.count + " 条任务");
    byId("batchForm").dataset.action = "/api/v1/tasks/batch";

    bindForm("groupForm", (fd) => ({
      id: fd.get("id"),
      name: fd.get("name"),
      agent_ids: splitCSV(fd.get("agent_ids")),
    }), (data) => "已保存分组 " + data.id);
    byId("groupForm").dataset.action = "/api/v1/groups";

    bindForm("uploadForm", (fd) => ({
      agent_id: fd.get("agent_id"),
      local_path: fd.get("local_path"),
      remote_path: fd.get("remote_path"),
    }), (data) => "已创建上传 " + data.transfer_id);
    byId("uploadForm").dataset.action = "/api/v1/files/upload";

    bindForm("downloadForm", (fd) => ({
      agent_id: fd.get("agent_id"),
      remote_path: fd.get("remote_path"),
      local_path: fd.get("local_path"),
    }), (data) => "已创建下载 " + data.transfer_id);
    byId("downloadForm").dataset.action = "/api/v1/files/download";

    refreshAll();
    setInterval(refreshAll, 5000);
  </script>
</body>
</html>`
