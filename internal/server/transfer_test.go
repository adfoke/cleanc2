package server

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cleanc2/internal/protocol"
)

func TestHandleTransferChunkThrottlesAuditWrites(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	dir := t.TempDir()
	tempPath := filepath.Join(dir, "download.part")
	file, err := os.Create(tempPath)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer file.Close()

	state := &transferState{
		ID:         "tx-1",
		AgentID:    "agent-1",
		Direction:  "download",
		LocalPath:  filepath.Join(dir, "out.txt"),
		RemotePath: "/tmp/out.txt",
		Status:     "running",
		CreatedAt:  time.Now().UTC(),
		tempPath:   tempPath,
		file:       file,
	}
	svc.putTransfer(state)
	svc.persistTransfer(state)

	svc.handleTransferChunk(protocol.FileTransferChunk{
		TransferID: state.ID,
		Data:       base64.StdEncoding.EncodeToString([]byte("abc")),
	})

	audit, ok, err := svc.store.TransferAudit(state.ID)
	if err != nil {
		t.Fatalf("read audit after first chunk: %v", err)
	}
	if !ok {
		t.Fatalf("expected persisted audit")
	}
	if audit.BytesTransferred != 0 {
		t.Fatalf("expected first small chunk to stay in memory, got %d", audit.BytesTransferred)
	}

	state.mu.Lock()
	state.lastPersistedAt = time.Now().Add(-transferAuditMinInterval - time.Millisecond)
	state.mu.Unlock()

	svc.handleTransferChunk(protocol.FileTransferChunk{
		TransferID: state.ID,
		Data:       base64.StdEncoding.EncodeToString([]byte("def")),
	})

	audit, ok, err = svc.store.TransferAudit(state.ID)
	if err != nil {
		t.Fatalf("read audit after second chunk: %v", err)
	}
	if !ok {
		t.Fatalf("expected persisted audit after throttle window")
	}
	if audit.BytesTransferred != 6 {
		t.Fatalf("expected persisted progress after throttle window, got %d", audit.BytesTransferred)
	}
}
