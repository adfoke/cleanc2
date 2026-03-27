package protocol

import (
	"encoding/json"
	"time"
)

const (
	TypeHello             = "hello"
	TypeHelloAck          = "hello_ack"
	TypeHeartbeat         = "heartbeat"
	TypeMetricsReport     = "metrics_report"
	TypeTaskDispatch      = "task_dispatch"
	TypeTaskAck           = "task_ack"
	TypeTaskCancel        = "task_cancel"
	TypeTaskResult        = "task_result"
	TypeFileTransferStart = "file_transfer_start"
	TypeFileTransferChunk = "file_transfer_chunk"
	TypeFileTransferDone  = "file_transfer_done"
	TypeError             = "error"
)

type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type AgentHello struct {
	AgentID     string    `json:"agent_id"`
	Token       string    `json:"token"`
	Hostname    string    `json:"hostname"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	IPAddrs     []string  `json:"ip_addrs,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Version     string    `json:"version"`
	ConnectedAt time.Time `json:"connected_at"`
}

type HelloAck struct {
	ServerTime   time.Time `json:"server_time"`
	AgentID      string    `json:"agent_id"`
	PendingTasks []Task    `json:"pending_tasks,omitempty"`
}

type Heartbeat struct {
	AgentID   string    `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
}

type MetricsReport struct {
	AgentID            string    `json:"agent_id"`
	Timestamp          time.Time `json:"timestamp"`
	UptimeSecs         int64     `json:"uptime_secs"`
	CPUCount           int       `json:"cpu_count"`
	Goroutines         int       `json:"goroutines"`
	ProcessMemoryBytes uint64    `json:"process_memory_bytes"`
	RootDiskTotalBytes uint64    `json:"root_disk_total_bytes"`
	RootDiskFreeBytes  uint64    `json:"root_disk_free_bytes"`
}

type Task struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	Type        string    `json:"type"`
	Command     string    `json:"command"`
	TimeoutSecs int       `json:"timeout_secs"`
	Priority    int       `json:"priority"`
	CreatedAt   time.Time `json:"created_at"`
}

type TaskAck struct {
	TaskID     string    `json:"task_id"`
	AgentID    string    `json:"agent_id"`
	ReceivedAt time.Time `json:"received_at"`
}

type TaskCancel struct {
	TaskID      string    `json:"task_id"`
	AgentID     string    `json:"agent_id"`
	RequestedAt time.Time `json:"requested_at"`
}

type TaskResult struct {
	TaskID      string    `json:"task_id"`
	AgentID     string    `json:"agent_id"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exit_code"`
	Stdout      string    `json:"stdout"`
	Stderr      string    `json:"stderr"`
	DurationMS  int64     `json:"duration_ms"`
	CompletedAt time.Time `json:"completed_at"`
}

type FileTransferStart struct {
	TransferID     string    `json:"transfer_id"`
	AgentID        string    `json:"agent_id"`
	Direction      string    `json:"direction"`
	LocalPath      string    `json:"local_path,omitempty"`
	RemotePath     string    `json:"remote_path"`
	Size           int64     `json:"size"`
	ChunkSize      int       `json:"chunk_size"`
	ChecksumSHA256 string    `json:"checksum_sha256,omitempty"`
	RequestedAt    time.Time `json:"requested_at"`
}

type FileTransferChunk struct {
	TransferID string `json:"transfer_id"`
	Seq        int    `json:"seq"`
	Data       string `json:"data"`
}

type FileTransferDone struct {
	TransferID     string    `json:"transfer_id"`
	AgentID        string    `json:"agent_id"`
	Direction      string    `json:"direction"`
	Status         string    `json:"status"`
	Message        string    `json:"message,omitempty"`
	Size           int64     `json:"size,omitempty"`
	ChecksumSHA256 string    `json:"checksum_sha256,omitempty"`
	CompletedAt    time.Time `json:"completed_at"`
}

type ErrorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
