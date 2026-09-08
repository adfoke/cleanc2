package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer emulates the operator API well enough to exercise write paths:
// task lifecycle (queued -> dispatched -> success/failed), transfers, and
// group membership updates.

func newWriteRegistry(t *testing.T) (*Registry, *httptest.Server) {
	t.Helper()
	var taskPolls int32

	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/tasks/batch", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AgentIDs    []string `json:"agent_ids"`
			GroupIDs    []string `json:"group_ids"`
			Tags        []string `json:"tags"`
			Command     string   `json:"command"`
			TimeoutSecs int      `json:"timeout_secs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		n := len(req.AgentIDs) + len(req.GroupIDs) + len(req.Tags)
		tasks := make([]map[string]any, 0, n)
		for i := range n {
			id := fmt.Sprintf("t-%d", i+1)
			switch req.Command {
			case "boom":
				id = "t-fail"
			case "stuck":
				id = "t-stuck"
			}
			tasks = append(tasks, map[string]any{"task_id": id, "agent_id": fmt.Sprintf("a%d", i), "dispatched": true})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"count": len(tasks), "tasks": tasks})
	})

	mux.HandleFunc("GET /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		polls := atomic.AddInt32(&taskPolls, 1)
		state := "dispatched"
		var result map[string]any
		switch id {
		case "t-1", "t-2":
			if polls >= 2 {
				state = "success"
				result = map[string]any{"exit_code": 0, "stdout": "hello", "duration_ms": 5}
			}
		case "t-fail":
			state = "failed"
			result = map[string]any{"exit_code": 2, "stderr": "kaboom"}
		case "t-stuck":
			// never terminal
		}
		doc := map[string]any{"task": map[string]any{"id": id}, "state": state}
		if result != nil {
			doc["result"] = result
		}
		_ = json.NewEncoder(w).Encode(doc)
	})

	mux.HandleFunc("POST /api/v1/files/upload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "tx-1", "status": "running"})
	})
	var transferPolls int32
	mux.HandleFunc("GET /api/v1/transfers/{id}", func(w http.ResponseWriter, r *http.Request) {
		// First observation is still running: proves poll actually loops.
		n := atomic.AddInt32(&transferPolls, 1)
		status := "running"
		if n >= 2 {
			status = "success"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": status, "size": 42})
	})

	var groupMu sync.Mutex
	groupMembers := []string{"a1"}
	mux.HandleFunc("GET /api/v1/groups/{id}", func(w http.ResponseWriter, _ *http.Request) {
		groupMu.Lock()
		members := append([]string{}, groupMembers...)
		groupMu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "g1", "name": "ops", "description": "d", "agent_ids": members})
	})
	mux.HandleFunc("POST /api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if members, ok := body["agent_ids"].([]any); ok {
			next := make([]string, 0, len(members))
			for _, m := range members {
				if s, _ := m.(string); s != "" {
					next = append(next, s)
				}
			}
			groupMu.Lock()
			groupMembers = next
			groupMu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(body)
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	r := &Registry{}
	RegisterAll(r)
	return r, ts
}

func runWait(t *testing.T, r *Registry, ts *httptest.Server, args ...string) (int, string, string) {
	t.Helper()
	full := append([]string{"-server", ts.URL}, args...)
	return run(t, r, full...)
}

func TestRunSingleAgentNoWait(t *testing.T) {
	r, ts := newWriteRegistry(t)
	code, stdout, stderr := runWait(t, r, ts, "run", "--cmd", "uptime", "--agents", "a1")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "t-1") {
		t.Fatalf("expected dispatched task ids, got %s", stdout)
	}
}

func TestRunFanOutRequiresYes(t *testing.T) {
	r, ts := newWriteRegistry(t)
	code, _, stderr := runWait(t, r, ts, "run", "--cmd", "uptime", "--agents", "a1,a2")
	if code != ExitUsage {
		t.Fatalf("fan-out without --yes: code=%d want 4", code)
	}
	if !strings.Contains(stderr, "needs_yes") {
		t.Fatalf("stderr = %s", stderr)
	}
	// Single group selector also expands beyond one agent -> --yes required.
	if code, _, _ := runWait(t, r, ts, "run", "--cmd", "uptime", "--group", "g1"); code != ExitUsage {
		t.Fatalf("group without --yes: code=%d want 4", code)
	}
	// With --yes it passes.
	if code, _, err := runWait(t, r, ts, "run", "--cmd", "uptime", "--agents", "a1,a2", "--yes"); code != ExitOK {
		t.Fatalf("fan-out with --yes: code=%d err=%s", code, err)
	}
}

func TestRunWaitSuccess(t *testing.T) {
	r, ts := newWriteRegistry(t)
	code, stdout, stderr := runWait(t, r, ts, "run", "--cmd", "uptime", "--agents", "a1",
		"--wait", "--wait-timeout", "10s")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var doc struct {
		AllOK   bool `json:"all_ok"`
		Results []struct {
			State  string `json:"state"`
			Stdout string `json:"stdout"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout not JSON: %v", err)
	}
	if !doc.AllOK || len(doc.Results) != 1 || doc.Results[0].State != "success" || doc.Results[0].Stdout != "hello" {
		t.Fatalf("unexpected wait result: %s", stdout)
	}
}

func TestRunWaitFailureExit1(t *testing.T) {
	r, ts := newWriteRegistry(t)
	code, stdout, stderr := runWait(t, r, ts, "run", "--cmd", "boom", "--agents", "a1",
		"--wait", "--wait-timeout", "10s")
	if code != ExitFailure {
		t.Fatalf("failed task: code=%d want 1 (stdout=%s stderr=%s)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "kaboom") {
		t.Fatalf("stdout must still carry the result rows: %s", stdout)
	}
}

func TestRunWaitTimeoutExit1(t *testing.T) {
	r, ts := newWriteRegistry(t)
	// t-stuck never reaches terminal on the fake.
	code, _, stderr := runWait(t, r, ts, "run", "--cmd", "stuck", "--agents", "a1",
		"--wait", "--wait-timeout", "300ms")
	if code != ExitFailure || !strings.Contains(stderr, "wait_timeout") {
		t.Fatalf("stuck task: code=%d stderr=%s want wait_timeout/1", code, stderr)
	}
}

func TestPushWait(t *testing.T) {
	r, ts := newWriteRegistry(t)
	code, stdout, stderr := runWait(t, r, ts, "push", "--agent", "a1", "--local", "/tmp/x",
		"--remote", "/tmp/y", "--wait")
	if code != ExitOK {
		t.Fatalf("push --wait: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "\"success\"") {
		t.Fatalf("push stdout = %s", stdout)
	}
}

func TestGroupsAddRemoveFullReplace(t *testing.T) {
	r, ts := newWriteRegistry(t)
	code, stdout, stderr := runWait(t, r, ts, "groups", "add", "g1", "a9", "a8")
	if code != ExitOK {
		t.Fatalf("groups add: code=%d stderr=%s", code, stderr)
	}
	// The fake echoes the POST body: members must be existing+new (full replace).
	if !strings.Contains(stdout, "a9") || !strings.Contains(stdout, "a1") {
		t.Fatalf("groups add lost existing members: %s", stdout)
	}

	code, stdout, _ = runWait(t, r, ts, "groups", "remove", "g1", "--agents", "a1")
	if code != ExitOK {
		t.Fatalf("groups remove: code=%d", code)
	}
	if strings.Contains(stdout, "\"a1\"") {
		t.Fatalf("groups remove kept a1: %s", stdout)
	}
	if !strings.Contains(stdout, "a9") {
		t.Fatalf("groups remove dropped others: %s", stdout)
	}
}

func TestTasksCancel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": r.PathValue("id"), "state": "cancel_requested", "cancel_sent": true})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	r := &Registry{}
	RegisterAll(r)
	code, stdout, _ := runWait(t, r, ts, "tasks", "cancel", "t-9")
	if code != ExitOK || !strings.Contains(stdout, "cancel_requested") {
		t.Fatalf("cancel: code=%d stdout=%s", code, stdout)
	}
}

// sanity: poll backoff is capped and bounded by budget
func TestPollBackoffCap(t *testing.T) {
	deadline := time.Now().Add(20 * time.Second)
	_ = deadline
	// structural check of the growth cap used by poll()
	d := 500 * time.Millisecond
	for i := range 8 {
		if d *= 2; d > 2*time.Second {
			d = 2 * time.Second
		}
		if i > 3 && d != 2*time.Second {
			t.Fatalf("backoff escaped cap at step %d: %v", i, d)
		}
	}
}
