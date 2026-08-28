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
		TransferStatus: TransferStatus{
			ID:         "tx-1",
			AgentID:    "agent-1",
			Direction:  "download",
			LocalPath:  filepath.Join(dir, "out.txt"),
			RemotePath: "/tmp/out.txt",
			Status:     "running",
			CreatedAt:  time.Now().UTC(),
		},
		tempPath: tempPath,
		file:     file,
	}
	svc.putTransfer(state)
	svc.persistTransfer(state)

	svc.handleTransferChunk(protocol.FileTransferChunk{
		TransferID: state.ID,
		Seq:        0,
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
		Seq:        1,
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

func TestHandleTransferResume(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	state := &transferState{
		TransferStatus: TransferStatus{ID: "tx-up", AgentID: "a1", Direction: "upload"},
		resumeCh:       make(chan int64, 1),
	}
	svc.putTransfer(state)

	svc.handleTransferResume(protocol.FileTransferResume{TransferID: "tx-up", Offset: 1024})

	select {
	case off := <-state.resumeCh:
		if off != 1024 {
			t.Fatalf("expected offset 1024, got %d", off)
		}
	default:
		t.Fatalf("expected resume offset to be delivered")
	}
}

func TestHandleTransferStartDownloadResume(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	dir := t.TempDir()
	tempPath := filepath.Join(dir, "out.part")
	// Pre-existing partial with two 4-byte chunks already persisted.
	if err := os.WriteFile(tempPath, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatalf("write partial: %v", err)
	}

	state := &transferState{
		TransferStatus: TransferStatus{
			ID:               "tx-resume",
			AgentID:          "agent-1",
			Direction:        "download",
			LocalPath:        filepath.Join(dir, "out.txt"),
			RemotePath:       "/tmp/out.txt",
			Status:           "requested",
			ChunkSize:        4,
			BytesTransferred: 8,
			CreatedAt:        time.Now().UTC(),
		},
		tempPath: tempPath,
	}
	svc.putTransfer(state)

	svc.handleTransferStart(protocol.FileTransferStart{
		TransferID: "tx-resume",
		Direction:  "download",
		Size:       12,
		ChunkSize:  4,
	})

	svc.handleTransferChunk(protocol.FileTransferChunk{
		TransferID: state.ID,
		Seq:        2,
		Data:       base64.StdEncoding.EncodeToString([]byte("ijkl")),
	})

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.file == nil {
		t.Fatalf("expected partial file to be opened")
	}
	if state.Size != 12 || state.expectedChunks != 3 {
		t.Fatalf("unexpected state: size=%d expectedChunks=%d", state.Size, state.expectedChunks)
	}
	if state.BytesTransferred != 12 {
		t.Fatalf("expected 12 bytes transferred, got %d", state.BytesTransferred)
	}
}

func TestReapStalledTransfers(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	now := time.Now().UTC()

	stalled := &transferState{
		TransferStatus: TransferStatus{
			ID:         "tx-stall",
			AgentID:    "agent-1",
			Direction:  "download",
			LocalPath:  filepath.Join(t.TempDir(), "out.txt"),
			RemotePath: "/tmp/out.txt",
			Status:     "running",
			CreatedAt:  now,
		},
		tempPath:        filepath.Join(t.TempDir(), "out.part"),
		lastPersistedAt: now.Add(-transferStallTimeout - time.Minute),
	}
	svc.putTransfer(stalled)

	fresh := &transferState{
		TransferStatus: TransferStatus{
			ID:         "tx-fresh",
			AgentID:    "agent-1",
			Direction:  "download",
			LocalPath:  filepath.Join(t.TempDir(), "fresh.txt"),
			RemotePath: "/tmp/fresh.txt",
			Status:     "running",
			CreatedAt:  now,
		},
		tempPath:        filepath.Join(t.TempDir(), "fresh.part"),
		lastPersistedAt: now,
	}
	svc.putTransfer(fresh)

	if n := svc.reapStalledTransfers(now); n != 1 {
		t.Fatalf("expected 1 reaped transfer, got %d", n)
	}
	if _, ok := svc.getTransfer(stalled.ID); ok {
		t.Fatalf("expected stalled transfer to be removed")
	}
	if _, ok := svc.getTransfer(fresh.ID); !ok {
		t.Fatalf("expected fresh transfer to remain")
	}

	audit, ok, err := svc.store.TransferAudit(stalled.ID)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !ok || audit.Status != "failed" {
		t.Fatalf("unexpected audit: %+v", audit)
	}
}
