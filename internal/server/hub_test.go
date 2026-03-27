package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cleanc2/internal/protocol"
	"go.uber.org/zap"
)

func TestBatchTaskRouteTargetsByTagsAndAgentIDs(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	if err := svc.store.UpsertAgent(AgentState{
		AgentID:     "agent-a",
		Hostname:    "a",
		OS:          "linux",
		Arch:        "amd64",
		Tags:        []string{"prod", "web"},
		Online:      false,
		LastSeenAt:  time.Now().UTC(),
		ConnectedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert agent-a: %v", err)
	}
	if err := svc.store.UpsertAgent(AgentState{
		AgentID:     "agent-b",
		Hostname:    "b",
		OS:          "linux",
		Arch:        "amd64",
		Tags:        []string{"ops"},
		Online:      false,
		LastSeenAt:  time.Now().UTC(),
		ConnectedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert agent-b: %v", err)
	}

	body := map[string]any{
		"agent_ids":    []string{"agent-b"},
		"tags":         []string{"prod"},
		"command":      "echo batch",
		"timeout_secs": 5,
	}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	setTestAuth(req)
	rec := httptest.NewRecorder()
	svc.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Count int `json:"count"`
		Tasks []struct {
			AgentID string `json:"agent_id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count != 2 {
		t.Fatalf("unexpected task count: %+v", resp)
	}
	if !(resp.Tasks[0].AgentID == "agent-a" && resp.Tasks[1].AgentID == "agent-b") {
		t.Fatalf("unexpected targets: %+v", resp.Tasks)
	}
}

func TestBatchTaskRouteTargetsByGroupIDs(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	for _, agentID := range []string{"agent-a", "agent-b"} {
		if err := svc.store.UpsertAgent(AgentState{
			AgentID:     agentID,
			Hostname:    agentID,
			OS:          "linux",
			Arch:        "amd64",
			Online:      false,
			LastSeenAt:  time.Now().UTC(),
			ConnectedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("upsert %s: %v", agentID, err)
		}
	}
	if err := svc.store.CreateOrUpdateGroup(Group{
		ID:        "group-1",
		Name:      "prod",
		AgentIDs:  []string{"agent-a", "agent-b"},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create group: %v", err)
	}

	raw, _ := json.Marshal(map[string]any{
		"group_ids":    []string{"group-1"},
		"command":      "echo from-group",
		"timeout_secs": 5,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/batch", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	setTestAuth(req)
	rec := httptest.NewRecorder()
	svc.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDashboardRouteReturnsHTML(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	setTestAuth(req)
	rec := httptest.NewRecorder()
	svc.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Contains(body, []byte("CleanC2 Dashboard")) {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func TestDashboardRouteRequiresAuth(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	svc.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDispatchKeepsTaskQueuedUntilAck(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	svc.clients["agent-1"] = &agentConn{
		id:      "agent-1",
		send:    make(chan outboundMessage, 1),
		service: svc,
	}

	resp, err := svc.createTask("agent-1", "echo ok", 5, 0)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if !resp.Dispatched {
		t.Fatalf("expected task to be queued for live client")
	}

	item, ok, err := svc.store.Task(resp.TaskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if !ok || item.State != "queued" {
		t.Fatalf("unexpected task state before ack: %+v", item)
	}

	if err := svc.store.MarkDispatched(resp.TaskID); err != nil {
		t.Fatalf("mark dispatched: %v", err)
	}

	item, ok, err = svc.store.Task(resp.TaskID)
	if err != nil {
		t.Fatalf("get task after ack: %v", err)
	}
	if !ok || item.State != "dispatched" {
		t.Fatalf("unexpected task state after ack: %+v", item)
	}
}

func TestHandleTransferChunkFailurePersistsAndClearsTransfer(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	state := &transferState{
		ID:         "tx-fail",
		AgentID:    "agent-1",
		Direction:  "download",
		LocalPath:  filepath.Join(t.TempDir(), "out.txt"),
		RemotePath: "/tmp/out.txt",
		Status:     "running",
		CreatedAt:  time.Now().UTC(),
	}
	svc.putTransfer(state)
	svc.handleTransferChunk(protocol.FileTransferChunk{
		TransferID: state.ID,
		Data:       base64.StdEncoding.EncodeToString([]byte("oops")),
	})

	if _, ok := svc.getTransfer(state.ID); ok {
		t.Fatalf("expected transfer to be cleared")
	}

	audit, ok, err := svc.store.TransferAudit(state.ID)
	if err != nil {
		t.Fatalf("get transfer audit: %v", err)
	}
	if !ok || audit.Status != "failed" {
		t.Fatalf("unexpected transfer audit: %+v", audit)
	}
}

func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()

	svc, err := New(Config{
		ListenAddr: ":0",
		AuthToken:  "test-token",
		DBPath:     filepath.Join(t.TempDir(), "test.db"),
		WriteWait:  2 * time.Second,
		PongWait:   2 * time.Second,
		PingPeriod: time.Second,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	return svc, func() {
		_ = svc.store.Close()
	}
}

func setTestAuth(req *http.Request) {
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:test-token")))
}
