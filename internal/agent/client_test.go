package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cleanc2/internal/protocol"
)

func TestCappedBufferTruncatesOutput(t *testing.T) {
	buf := &cappedBuffer{limit: 5}
	if _, err := buf.Write([]byte("hello")); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	if _, err := buf.Write([]byte(" world")); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}

	if got := buf.String(); got != "hello\n[output truncated]" {
		t.Fatalf("unexpected buffer output: %q", got)
	}
}

func TestCacheResultPrunesExpiredAndOldest(t *testing.T) {
	client := &Client{
		results: make(map[string]cachedTaskResult),
	}

	client.results["expired"] = cachedTaskResult{
		result:   protocol.TaskResult{TaskID: "expired"},
		cachedAt: time.Now().Add(-cachedResultTTL - time.Second),
	}
	for i := range maxCachedResults {
		taskID := fmt.Sprintf("task-%03d", i)
		client.results[taskID] = cachedTaskResult{
			result:   protocol.TaskResult{TaskID: taskID},
			cachedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		}
	}

	client.cacheResult(protocol.TaskResult{TaskID: "fresh"})

	if len(client.results) != maxCachedResults {
		t.Fatalf("unexpected cache size: %d", len(client.results))
	}
	if _, ok := client.results["expired"]; ok {
		t.Fatalf("expired result should be pruned")
	}
	if _, ok := client.results["task-000"]; ok {
		t.Fatalf("oldest result should be evicted")
	}
	if _, ok := client.results["fresh"]; !ok {
		t.Fatalf("new result should be cached")
	}
}

func TestFinalizeUploadChecksumMismatchRemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "file.txt.part.tx")
	remotePath := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(tempPath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	client := &Client{agentID: "agent-1"}
	status := client.finalizeUpload(&uploadState{
		tempPath:   tempPath,
		remotePath: remotePath,
		received:   int64(len("payload")),
	}, protocol.FileTransferDone{
		TransferID:     "tx",
		Direction:      "upload",
		Status:         "complete",
		ChecksumSHA256: "mismatch",
	})

	if status.Status != "failed" || status.Message != "checksum mismatch" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed, err=%v", err)
	}
	if _, err := os.Stat(remotePath); !os.IsNotExist(err) {
		t.Fatalf("remote file should not exist, err=%v", err)
	}
}

func TestRunCommandSuccess(t *testing.T) {
	client := &Client{agentID: "agent-1"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := client.runCommand(ctx, protocol.Task{ID: "t1", Command: "echo hello"})
	if result.Status != "success" || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Fatalf("unexpected stdout: %q", result.Stdout)
	}
}

func TestRunCommandTimeout(t *testing.T) {
	client := &Client{agentID: "agent-1"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	result := client.runCommand(ctx, protocol.Task{ID: "t2", Command: "sleep 5"})
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got %+v", result)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
}

func TestRunCommandCancel(t *testing.T) {
	client := &Client{agentID: "agent-1"}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result := client.runCommand(ctx, protocol.Task{ID: "t3", Command: "sleep 5"})
	if result.Status != "canceled" {
		t.Fatalf("expected canceled, got %+v", result)
	}
}
