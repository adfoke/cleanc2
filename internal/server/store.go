package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	_ "modernc.org/sqlite"

	"cleanc2/internal/protocol"
)

type AgentState struct {
	AgentID      string    `json:"agent_id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	Tags         []string  `json:"tags,omitempty"`
	Fingerprint  string    `json:"fingerprint,omitempty"`
	Online       bool      `json:"online"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	ConnectedAt  time.Time `json:"connected_at"`
	PendingCount int       `json:"pending_count"`
}

type Group struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	AgentIDs    []string  `json:"agent_ids,omitempty"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type AgentMetrics struct {
	AgentID            string    `json:"agent_id"`
	Timestamp          time.Time `json:"timestamp"`
	UptimeSecs         int64     `json:"uptime_secs"`
	CPUCount           int       `json:"cpu_count"`
	Goroutines         int       `json:"goroutines"`
	ProcessMemoryBytes uint64    `json:"process_memory_bytes"`
	RootDiskTotalBytes uint64    `json:"root_disk_total_bytes"`
	RootDiskFreeBytes  uint64    `json:"root_disk_free_bytes"`
}

type TransferAudit struct {
	TransferID       string    `json:"transfer_id"`
	AgentID          string    `json:"agent_id"`
	Direction        string    `json:"direction"`
	LocalPath        string    `json:"local_path,omitempty"`
	RemotePath       string    `json:"remote_path"`
	Status           string    `json:"status"`
	Message          string    `json:"message,omitempty"`
	Size             int64     `json:"size"`
	BytesTransferred int64     `json:"bytes_transferred"`
	ChecksumSHA256   string    `json:"checksum_sha256,omitempty"`
	ChecksumVerified bool      `json:"checksum_verified"`
	CreatedAt        time.Time `json:"created_at"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
}

type taskStatus struct {
	Task   protocol.Task        `json:"task"`
	Result *protocol.TaskResult `json:"result,omitempty"`
	State  string               `json:"state"`
}

type Store struct {
	db *sql.DB
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		path = "cleanc2.db"
	}
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.ResetOnlineAgents(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) init() error {
	stmts := []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS agents (
			agent_id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL DEFAULT '',
			os TEXT NOT NULL DEFAULT '',
			arch TEXT NOT NULL DEFAULT '',
			tags_json TEXT NOT NULL DEFAULT '[]',
			fingerprint TEXT NOT NULL DEFAULT '',
			online INTEGER NOT NULL DEFAULT 0,
			last_seen_at TEXT,
			connected_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			type TEXT NOT NULL,
			command TEXT NOT NULL,
			timeout_secs INTEGER NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			state TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS task_results (
			task_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			status TEXT NOT NULL,
			exit_code INTEGER NOT NULL,
			stdout TEXT NOT NULL DEFAULT '',
			stderr TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			completed_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS group_members (
			group_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			PRIMARY KEY(group_id, agent_id),
			FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS agent_metrics (
			agent_id TEXT PRIMARY KEY,
			timestamp TEXT NOT NULL,
			uptime_secs INTEGER NOT NULL,
			cpu_count INTEGER NOT NULL,
			goroutines INTEGER NOT NULL,
			process_memory_bytes INTEGER NOT NULL,
			root_disk_total_bytes INTEGER NOT NULL,
			root_disk_free_bytes INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS transfer_audit (
			transfer_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			direction TEXT NOT NULL,
			local_path TEXT NOT NULL DEFAULT '',
			remote_path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			bytes_transferred INTEGER NOT NULL DEFAULT 0,
			checksum_sha256 TEXT NOT NULL DEFAULT '',
			checksum_verified INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			completed_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_agent_state_created ON tasks(agent_id, state, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_group_members_agent ON group_members(agent_id);`,
		`CREATE INDEX IF NOT EXISTS idx_transfer_audit_agent_created ON transfer_audit(agent_id, created_at);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init sqlite: %w", err)
		}
	}
	return nil
}

func (s *Store) ResetOnlineAgents() error {
	_, err := s.db.Exec(`UPDATE agents SET online = 0`)
	return err
}

func (s *Store) UpsertAgent(state AgentState) error {
	tagsJSON, err := json.Marshal(state.Tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO agents(agent_id, hostname, os, arch, tags_json, fingerprint, online, last_seen_at, connected_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			hostname = excluded.hostname,
			os = excluded.os,
			arch = excluded.arch,
			tags_json = excluded.tags_json,
			fingerprint = excluded.fingerprint,
			online = excluded.online,
			last_seen_at = excluded.last_seen_at,
			connected_at = excluded.connected_at
	`,
		state.AgentID,
		state.Hostname,
		state.OS,
		state.Arch,
		string(tagsJSON),
		state.Fingerprint,
		boolToInt(state.Online),
		formatNullTime(state.LastSeenAt),
		formatNullTime(state.ConnectedAt),
	)
	return err
}

func (s *Store) SetAgentOnline(agentID string, online bool, seenAt time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO agents(agent_id, online, last_seen_at)
		VALUES(?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			online = excluded.online,
			last_seen_at = excluded.last_seen_at
	`, agentID, boolToInt(online), formatNullTime(seenAt))
	return err
}

func (s *Store) Agents() ([]AgentState, error) {
	rows, err := s.db.Query(`
		SELECT
			a.agent_id,
			a.hostname,
			a.os,
			a.arch,
			a.tags_json,
			a.fingerprint,
			a.online,
			COALESCE(a.last_seen_at, ''),
			COALESCE(a.connected_at, ''),
			COALESCE(SUM(CASE WHEN t.state = 'queued' THEN 1 ELSE 0 END), 0) AS pending_count
		FROM agents a
		LEFT JOIN tasks t ON t.agent_id = a.agent_id
		GROUP BY a.agent_id, a.hostname, a.os, a.arch, a.tags_json, a.fingerprint, a.online, a.last_seen_at, a.connected_at
		ORDER BY a.agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []AgentState
	for rows.Next() {
		var (
			item        AgentState
			tagsJSON    string
			onlineInt   int
			lastSeenRaw string
			connRaw     string
		)
		if err := rows.Scan(
			&item.AgentID,
			&item.Hostname,
			&item.OS,
			&item.Arch,
			&tagsJSON,
			&item.Fingerprint,
			&onlineInt,
			&lastSeenRaw,
			&connRaw,
			&item.PendingCount,
		); err != nil {
			return nil, err
		}
		item.Online = onlineInt == 1
		item.Tags = decodeTags(tagsJSON)
		item.LastSeenAt = parseNullTime(lastSeenRaw)
		item.ConnectedAt = parseNullTime(connRaw)
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Store) AddTask(task protocol.Task) error {
	_, err := s.db.Exec(`
		INSERT INTO tasks(id, agent_id, type, command, timeout_secs, priority, created_at, state)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			agent_id = excluded.agent_id,
			type = excluded.type,
			command = excluded.command,
			timeout_secs = excluded.timeout_secs,
			priority = excluded.priority,
			created_at = excluded.created_at,
			state = excluded.state
	`, task.ID, task.AgentID, task.Type, task.Command, task.TimeoutSecs, task.Priority, task.CreatedAt.UTC().Format(time.RFC3339Nano), "queued")
	return err
}

func (s *Store) PendingTasks(agentID string) ([]protocol.Task, error) {
	rows, err := s.db.Query(`
			SELECT id, agent_id, type, command, timeout_secs, priority, created_at
			FROM tasks
			WHERE agent_id = ? AND state = 'queued'
			ORDER BY priority DESC, created_at ASC
		`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []protocol.Task
	for rows.Next() {
		var (
			item       protocol.Task
			createdRaw string
		)
		if err := rows.Scan(&item.ID, &item.AgentID, &item.Type, &item.Command, &item.TimeoutSecs, &item.Priority, &createdRaw); err != nil {
			return nil, err
		}
		item.CreatedAt = parseNullTime(createdRaw)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkDispatched(taskID string) error {
	_, err := s.db.Exec(`UPDATE tasks SET state = 'dispatched' WHERE id = ? AND state = 'queued'`, taskID)
	return err
}

func (s *Store) SaveResult(result protocol.TaskResult) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE tasks SET state = ? WHERE id = ?`, result.Status, result.TaskID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO task_results(task_id, agent_id, status, exit_code, stdout, stderr, duration_ms, completed_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			agent_id = excluded.agent_id,
			status = excluded.status,
			exit_code = excluded.exit_code,
			stdout = excluded.stdout,
			stderr = excluded.stderr,
			duration_ms = excluded.duration_ms,
			completed_at = excluded.completed_at
	`, result.TaskID, result.AgentID, result.Status, result.ExitCode, result.Stdout, result.Stderr, result.DurationMS, result.CompletedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) Task(taskID string) (taskStatus, bool, error) {
	row := s.db.QueryRow(`
		SELECT
			t.id,
			t.agent_id,
			t.type,
			t.command,
			t.timeout_secs,
			t.priority,
			t.created_at,
			t.state,
			COALESCE(r.task_id, ''),
			COALESCE(r.agent_id, ''),
			COALESCE(r.status, ''),
			COALESCE(r.exit_code, 0),
			COALESCE(r.stdout, ''),
			COALESCE(r.stderr, ''),
			COALESCE(r.duration_ms, 0),
			COALESCE(r.completed_at, '')
		FROM tasks t
		LEFT JOIN task_results r ON r.task_id = t.id
		WHERE t.id = ?
	`, taskID)

	var (
		item          taskStatus
		createdRaw    string
		resultTaskID  string
		resultAgentID string
		resultStatus  string
		resultExit    int
		resultStdout  string
		resultStderr  string
		resultDurMS   int64
		completedRaw  string
	)
	if err := row.Scan(
		&item.Task.ID,
		&item.Task.AgentID,
		&item.Task.Type,
		&item.Task.Command,
		&item.Task.TimeoutSecs,
		&item.Task.Priority,
		&createdRaw,
		&item.State,
		&resultTaskID,
		&resultAgentID,
		&resultStatus,
		&resultExit,
		&resultStdout,
		&resultStderr,
		&resultDurMS,
		&completedRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return taskStatus{}, false, nil
		}
		return taskStatus{}, false, err
	}

	item.Task.CreatedAt = parseNullTime(createdRaw)
	if resultTaskID != "" {
		item.Result = &protocol.TaskResult{
			TaskID:      resultTaskID,
			AgentID:     resultAgentID,
			Status:      resultStatus,
			ExitCode:    resultExit,
			Stdout:      resultStdout,
			Stderr:      resultStderr,
			DurationMS:  resultDurMS,
			CompletedAt: parseNullTime(completedRaw),
		}
	}
	return item, true, nil
}

func (s *Store) RecentTasks(limit int) ([]taskStatus, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(`
		SELECT
			t.id,
			t.agent_id,
			t.type,
			t.command,
			t.timeout_secs,
			t.priority,
			t.created_at,
			t.state,
			COALESCE(r.task_id, ''),
			COALESCE(r.agent_id, ''),
			COALESCE(r.status, ''),
			COALESCE(r.exit_code, 0),
			COALESCE(r.stdout, ''),
			COALESCE(r.stderr, ''),
			COALESCE(r.duration_ms, 0),
			COALESCE(r.completed_at, '')
		FROM tasks t
		LEFT JOIN task_results r ON r.task_id = t.id
		ORDER BY t.created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []taskStatus
	for rows.Next() {
		item, err := scanTaskStatus(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveAgentMetrics(metrics AgentMetrics) error {
	_, err := s.db.Exec(`
		INSERT INTO agent_metrics(agent_id, timestamp, uptime_secs, cpu_count, goroutines, process_memory_bytes, root_disk_total_bytes, root_disk_free_bytes)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			timestamp = excluded.timestamp,
			uptime_secs = excluded.uptime_secs,
			cpu_count = excluded.cpu_count,
			goroutines = excluded.goroutines,
			process_memory_bytes = excluded.process_memory_bytes,
			root_disk_total_bytes = excluded.root_disk_total_bytes,
			root_disk_free_bytes = excluded.root_disk_free_bytes
	`, metrics.AgentID, metrics.Timestamp.UTC().Format(time.RFC3339Nano), metrics.UptimeSecs, metrics.CPUCount, metrics.Goroutines, metrics.ProcessMemoryBytes, metrics.RootDiskTotalBytes, metrics.RootDiskFreeBytes)
	return err
}

func (s *Store) AgentMetrics(agentID string) (AgentMetrics, bool, error) {
	row := s.db.QueryRow(`
		SELECT agent_id, timestamp, uptime_secs, cpu_count, goroutines, process_memory_bytes, root_disk_total_bytes, root_disk_free_bytes
		FROM agent_metrics
		WHERE agent_id = ?
	`, agentID)

	var metrics AgentMetrics
	var ts string
	if err := row.Scan(&metrics.AgentID, &ts, &metrics.UptimeSecs, &metrics.CPUCount, &metrics.Goroutines, &metrics.ProcessMemoryBytes, &metrics.RootDiskTotalBytes, &metrics.RootDiskFreeBytes); err != nil {
		if err == sql.ErrNoRows {
			return AgentMetrics{}, false, nil
		}
		return AgentMetrics{}, false, err
	}
	metrics.Timestamp = parseNullTime(ts)
	return metrics, true, nil
}

func (s *Store) UpsertTransferAudit(audit TransferAudit) error {
	_, err := s.db.Exec(`
		INSERT INTO transfer_audit(transfer_id, agent_id, direction, local_path, remote_path, status, message, size, bytes_transferred, checksum_sha256, checksum_verified, created_at, completed_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(transfer_id) DO UPDATE SET
			agent_id = excluded.agent_id,
			direction = excluded.direction,
			local_path = excluded.local_path,
			remote_path = excluded.remote_path,
			status = excluded.status,
			message = excluded.message,
			size = excluded.size,
			bytes_transferred = excluded.bytes_transferred,
			checksum_sha256 = excluded.checksum_sha256,
			checksum_verified = excluded.checksum_verified,
			created_at = excluded.created_at,
			completed_at = excluded.completed_at
	`, audit.TransferID, audit.AgentID, audit.Direction, audit.LocalPath, audit.RemotePath, audit.Status, audit.Message, audit.Size, audit.BytesTransferred, audit.ChecksumSHA256, boolToInt(audit.ChecksumVerified), audit.CreatedAt.UTC().Format(time.RFC3339Nano), formatNullTime(audit.CompletedAt))
	return err
}

func (s *Store) TransferAudit(transferID string) (TransferAudit, bool, error) {
	row := s.db.QueryRow(`
		SELECT transfer_id, agent_id, direction, local_path, remote_path, status, message, size, bytes_transferred, checksum_sha256, checksum_verified, created_at, COALESCE(completed_at, '')
		FROM transfer_audit
		WHERE transfer_id = ?
	`, transferID)

	var (
		audit      TransferAudit
		verified   int
		createdRaw string
		doneRaw    string
	)
	if err := row.Scan(&audit.TransferID, &audit.AgentID, &audit.Direction, &audit.LocalPath, &audit.RemotePath, &audit.Status, &audit.Message, &audit.Size, &audit.BytesTransferred, &audit.ChecksumSHA256, &verified, &createdRaw, &doneRaw); err != nil {
		if err == sql.ErrNoRows {
			return TransferAudit{}, false, nil
		}
		return TransferAudit{}, false, err
	}
	audit.ChecksumVerified = verified == 1
	audit.CreatedAt = parseNullTime(createdRaw)
	audit.CompletedAt = parseNullTime(doneRaw)
	return audit, true, nil
}

func (s *Store) RecentTransferAudits(limit int) ([]TransferAudit, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(`
		SELECT transfer_id, agent_id, direction, local_path, remote_path, status, message, size, bytes_transferred, checksum_sha256, checksum_verified, created_at, COALESCE(completed_at, '')
		FROM transfer_audit
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TransferAudit
	for rows.Next() {
		var (
			audit      TransferAudit
			verified   int
			createdRaw string
			doneRaw    string
		)
		if err := rows.Scan(&audit.TransferID, &audit.AgentID, &audit.Direction, &audit.LocalPath, &audit.RemotePath, &audit.Status, &audit.Message, &audit.Size, &audit.BytesTransferred, &audit.ChecksumSHA256, &verified, &createdRaw, &doneRaw); err != nil {
			return nil, err
		}
		audit.ChecksumVerified = verified == 1
		audit.CreatedAt = parseNullTime(createdRaw)
		audit.CompletedAt = parseNullTime(doneRaw)
		items = append(items, audit)
	}
	return items, rows.Err()
}

func (s *Store) CreateOrUpdateGroup(group Group) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO groups(id, name, description, created_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description
	`, group.ID, group.Name, group.Description, group.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM group_members WHERE group_id = ?`, group.ID); err != nil {
		return err
	}
	for _, agentID := range group.AgentIDs {
		if _, err := tx.Exec(`INSERT INTO group_members(group_id, agent_id) VALUES(?, ?)`, group.ID, agentID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Groups() ([]Group, error) {
	rows, err := s.db.Query(`
		SELECT g.id, g.name, g.description, g.created_at, COUNT(m.agent_id)
		FROM groups g
		LEFT JOIN group_members m ON m.group_id = g.id
		GROUP BY g.id, g.name, g.description, g.created_at
		ORDER BY g.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Group
	for rows.Next() {
		var item Group
		var createdRaw string
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &createdRaw, &item.MemberCount); err != nil {
			return nil, err
		}
		item.CreatedAt = parseNullTime(createdRaw)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Group(groupID string) (Group, bool, error) {
	row := s.db.QueryRow(`SELECT id, name, description, created_at FROM groups WHERE id = ?`, groupID)

	var group Group
	var createdRaw string
	if err := row.Scan(&group.ID, &group.Name, &group.Description, &createdRaw); err != nil {
		if err == sql.ErrNoRows {
			return Group{}, false, nil
		}
		return Group{}, false, err
	}
	group.CreatedAt = parseNullTime(createdRaw)

	rows, err := s.db.Query(`SELECT agent_id FROM group_members WHERE group_id = ? ORDER BY agent_id`, groupID)
	if err != nil {
		return Group{}, false, err
	}
	defer rows.Close()

	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			return Group{}, false, err
		}
		group.AgentIDs = append(group.AgentIDs, agentID)
	}
	group.MemberCount = len(group.AgentIDs)
	return group, true, rows.Err()
}

func (s *Store) ResolveGroupAgentIDs(groupIDs []string) ([]string, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}

	set := make(map[string]struct{})
	for _, groupID := range groupIDs {
		rows, err := s.db.Query(`SELECT agent_id FROM group_members WHERE group_id = ?`, groupID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var agentID string
			if err := rows.Scan(&agentID); err != nil {
				rows.Close()
				return nil, err
			}
			set[agentID] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	agentIDs := make([]string, 0, len(set))
	for agentID := range set {
		agentIDs = append(agentIDs, agentID)
	}
	return agentIDs, nil
}

func (s *Store) CancelTask(taskID string) (protocol.Task, string, bool, error) {
	task, state, ok, err := s.taskRow(taskID)
	if err != nil || !ok {
		return protocol.Task{}, "", ok, err
	}

	switch state {
	case "success", "failed", "timeout", "canceled":
		return task, state, true, nil
	case "queued":
		now := time.Now().UTC()
		tx, err := s.db.Begin()
		if err != nil {
			return protocol.Task{}, "", false, err
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`UPDATE tasks SET state = 'canceled' WHERE id = ?`, taskID); err != nil {
			return protocol.Task{}, "", false, err
		}
		if _, err := tx.Exec(`
			INSERT INTO task_results(task_id, agent_id, status, exit_code, stdout, stderr, duration_ms, completed_at)
			VALUES(?, ?, 'canceled', 0, '', 'canceled before dispatch', 0, ?)
			ON CONFLICT(task_id) DO UPDATE SET
				status = excluded.status,
				stderr = excluded.stderr,
				completed_at = excluded.completed_at
		`, taskID, task.AgentID, now.Format(time.RFC3339Nano)); err != nil {
			return protocol.Task{}, "", false, err
		}
		if err := tx.Commit(); err != nil {
			return protocol.Task{}, "", false, err
		}
		return task, "canceled", true, nil
	default:
		_, err := s.db.Exec(`UPDATE tasks SET state = 'cancel_requested' WHERE id = ?`, taskID)
		return task, "cancel_requested", true, err
	}
}

func (s *Store) taskRow(taskID string) (protocol.Task, string, bool, error) {
	row := s.db.QueryRow(`
		SELECT id, agent_id, type, command, timeout_secs, priority, created_at, state
		FROM tasks
		WHERE id = ?
	`, taskID)

	var task protocol.Task
	var createdRaw string
	var state string
	if err := row.Scan(&task.ID, &task.AgentID, &task.Type, &task.Command, &task.TimeoutSecs, &task.Priority, &createdRaw, &state); err != nil {
		if err == sql.ErrNoRows {
			return protocol.Task{}, "", false, nil
		}
		return protocol.Task{}, "", false, err
	}
	task.CreatedAt = parseNullTime(createdRaw)
	return task, state, true, nil
}

func scanTaskStatus(scanner interface {
	Scan(dest ...any) error
}) (taskStatus, error) {
	var (
		item          taskStatus
		createdRaw    string
		resultTaskID  string
		resultAgentID string
		resultStatus  string
		resultExit    int
		resultStdout  string
		resultStderr  string
		resultDurMS   int64
		completedRaw  string
	)
	if err := scanner.Scan(
		&item.Task.ID,
		&item.Task.AgentID,
		&item.Task.Type,
		&item.Task.Command,
		&item.Task.TimeoutSecs,
		&item.Task.Priority,
		&createdRaw,
		&item.State,
		&resultTaskID,
		&resultAgentID,
		&resultStatus,
		&resultExit,
		&resultStdout,
		&resultStderr,
		&resultDurMS,
		&completedRaw,
	); err != nil {
		return taskStatus{}, err
	}

	item.Task.CreatedAt = parseNullTime(createdRaw)
	if resultTaskID != "" {
		item.Result = &protocol.TaskResult{
			TaskID:      resultTaskID,
			AgentID:     resultAgentID,
			Status:      resultStatus,
			ExitCode:    resultExit,
			Stdout:      resultStdout,
			Stderr:      resultStderr,
			DurationMS:  resultDurMS,
			CompletedAt: parseNullTime(completedRaw),
		}
	}
	return item, nil
}

func matchAgentTags(agentTags, filterTags []string) bool {
	if len(filterTags) == 0 {
		return true
	}
	for _, tag := range agentTags {
		if slices.Contains(filterTags, tag) {
			return true
		}
	}
	return false
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" || path == ":memory:" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func decodeTags(raw string) []string {
	if raw == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil
	}
	return tags
}

func parseNullTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formatNullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
