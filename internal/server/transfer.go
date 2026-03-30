package server

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"cleanc2/internal/common"
	"cleanc2/internal/protocol"
)

const (
	transferAuditMinInterval = time.Second
	transferAuditMinBytes    = 1 << 20
)

type TransferStatus struct {
	ID               string    `json:"transfer_id"`
	AgentID          string    `json:"agent_id"`
	Direction        string    `json:"direction"`
	LocalPath        string    `json:"local_path,omitempty"`
	RemotePath       string    `json:"remote_path"`
	Status           string    `json:"status"`
	Message          string    `json:"message,omitempty"`
	Size             int64     `json:"size"`
	BytesTransferred int64     `json:"bytes_transferred"`
	ChunkSize        int       `json:"chunk_size"`
	ChecksumSHA256   string    `json:"checksum_sha256,omitempty"`
	ChecksumVerified bool      `json:"checksum_verified"`
	CreatedAt        time.Time `json:"created_at"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
}

type transferState struct {
	ID               string    `json:"transfer_id"`
	AgentID          string    `json:"agent_id"`
	Direction        string    `json:"direction"`
	LocalPath        string    `json:"local_path,omitempty"`
	RemotePath       string    `json:"remote_path"`
	Status           string    `json:"status"`
	Message          string    `json:"message,omitempty"`
	Size             int64     `json:"size"`
	BytesTransferred int64     `json:"bytes_transferred"`
	ChunkSize        int       `json:"chunk_size"`
	ChecksumSHA256   string    `json:"checksum_sha256,omitempty"`
	ChecksumVerified bool      `json:"checksum_verified"`
	CreatedAt        time.Time `json:"created_at"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`

	tempPath string
	file     *os.File
	mu       sync.Mutex

	lastPersistedAt    time.Time
	lastPersistedBytes int64
}

func (t *transferState) snapshot() TransferStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

func (t *transferState) snapshotLocked() TransferStatus {
	return TransferStatus{
		ID:               t.ID,
		AgentID:          t.AgentID,
		Direction:        t.Direction,
		LocalPath:        t.LocalPath,
		RemotePath:       t.RemotePath,
		Status:           t.Status,
		Message:          t.Message,
		Size:             t.Size,
		BytesTransferred: t.BytesTransferred,
		ChunkSize:        t.ChunkSize,
		ChecksumSHA256:   t.ChecksumSHA256,
		ChecksumVerified: t.ChecksumVerified,
		CreatedAt:        t.CreatedAt,
		CompletedAt:      t.CompletedAt,
	}
}

func (s *Service) startUpload(agentID, localPath, remotePath string, chunkSize int) (TransferStatus, error) {
	client, err := s.clientForAgent(agentID)
	if err != nil {
		return TransferStatus{}, err
	}
	if chunkSize <= 0 {
		chunkSize = 256 * 1024
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return TransferStatus{}, err
	}
	if info.IsDir() {
		return TransferStatus{}, errors.New("local_path must be a file")
	}

	state := &transferState{
		ID:         common.NewID(),
		AgentID:    agentID,
		Direction:  "upload",
		LocalPath:  localPath,
		RemotePath: remotePath,
		Status:     "queued",
		Size:       info.Size(),
		ChunkSize:  chunkSize,
		CreatedAt:  time.Now().UTC(),
	}
	s.putTransfer(state)
	s.persistTransfer(state)

	go s.runUpload(client, state)
	return state.snapshot(), nil
}

func (s *Service) startDownload(agentID, remotePath, localPath string, chunkSize int) (TransferStatus, error) {
	client, err := s.clientForAgent(agentID)
	if err != nil {
		return TransferStatus{}, err
	}
	if chunkSize <= 0 {
		chunkSize = 256 * 1024
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return TransferStatus{}, err
	}

	state := &transferState{
		ID:         common.NewID(),
		AgentID:    agentID,
		Direction:  "download",
		LocalPath:  localPath,
		RemotePath: remotePath,
		Status:     "requested",
		ChunkSize:  chunkSize,
		CreatedAt:  time.Now().UTC(),
		tempPath:   localPath + ".part." + common.NewID(),
	}
	s.putTransfer(state)
	s.persistTransfer(state)

	start := protocol.FileTransferStart{
		TransferID:  state.ID,
		AgentID:     agentID,
		Direction:   "download",
		LocalPath:   localPath,
		RemotePath:  remotePath,
		ChunkSize:   chunkSize,
		RequestedAt: time.Now().UTC(),
	}
	if err := client.sendMessage(protocol.TypeFileTransferStart, start); err != nil {
		s.finishTransferWithError(state, err)
		return TransferStatus{}, err
	}

	return state.snapshot(), nil
}

func (s *Service) runUpload(client *agentConn, state *transferState) {
	file, err := os.Open(state.LocalPath)
	if err != nil {
		s.finishTransferWithError(state, err)
		return
	}
	defer file.Close()

	checksum, err := common.FileSHA256(state.LocalPath)
	if err != nil {
		s.finishTransferWithError(state, err)
		return
	}

	state.mu.Lock()
	state.Status = "running"
	state.ChecksumSHA256 = checksum
	state.mu.Unlock()
	s.persistTransfer(state)

	start := protocol.FileTransferStart{
		TransferID:     state.ID,
		AgentID:        state.AgentID,
		Direction:      state.Direction,
		LocalPath:      state.LocalPath,
		RemotePath:     state.RemotePath,
		Size:           state.Size,
		ChunkSize:      state.ChunkSize,
		ChecksumSHA256: checksum,
		RequestedAt:    time.Now().UTC(),
	}
	if err := client.sendMessage(protocol.TypeFileTransferStart, start); err != nil {
		s.finishTransferWithError(state, err)
		return
	}

	buf := make([]byte, state.ChunkSize)
	seq := 0
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			chunk := protocol.FileTransferChunk{
				TransferID: state.ID,
				Seq:        seq,
				Data:       base64.StdEncoding.EncodeToString(buf[:n]),
			}
			if err := client.sendMessage(protocol.TypeFileTransferChunk, chunk); err != nil {
				s.finishTransferWithError(state, err)
				return
			}
			state.mu.Lock()
			state.BytesTransferred += int64(n)
			if s.shouldPersistTransferProgressLocked(state, time.Now().UTC()) {
				s.persistTransferLocked(state)
			}
			state.mu.Unlock()
			seq++
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			if errors.Is(readErr, os.ErrClosed) {
				s.finishTransferWithError(state, readErr)
				return
			}
			s.finishTransferWithError(state, readErr)
			return
		}
	}

	state.mu.Lock()
	state.Status = "waiting_remote"
	state.mu.Unlock()
	s.persistTransfer(state)

	if err := client.sendMessage(protocol.TypeFileTransferDone, protocol.FileTransferDone{
		TransferID:     state.ID,
		AgentID:        state.AgentID,
		Direction:      state.Direction,
		Status:         "complete",
		Size:           state.Size,
		ChecksumSHA256: checksum,
		CompletedAt:    time.Now().UTC(),
	}); err != nil {
		s.finishTransferWithError(state, err)
	}
}

func (s *Service) handleTransferStart(msg protocol.FileTransferStart) {
	state, ok := s.getTransfer(msg.TransferID)
	if !ok || state.Direction != "download" {
		return
	}

	var fail error
	state.mu.Lock()
	if state.file == nil {
		file, err := os.Create(state.tempPath)
		if err != nil {
			fail = err
		} else {
			state.file = file
		}
	}
	if fail == nil {
		state.Status = "running"
		state.Size = msg.Size
		s.persistTransferLocked(state)
	}
	state.mu.Unlock()
	if fail != nil {
		s.finishTransferWithError(state, fail)
	}
}

func (s *Service) handleTransferChunk(msg protocol.FileTransferChunk) {
	state, ok := s.getTransfer(msg.TransferID)
	if !ok || state.Direction != "download" {
		return
	}

	data, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		s.finishTransferWithError(state, err)
		return
	}

	state.mu.Lock()
	if state.file == nil {
		state.mu.Unlock()
		s.finishTransferWithError(state, errors.New("download file not initialized"))
		return
	}
	if _, err := state.file.Write(data); err != nil {
		state.mu.Unlock()
		s.finishTransferWithError(state, err)
		return
	}
	state.BytesTransferred += int64(len(data))
	if s.shouldPersistTransferProgressLocked(state, time.Now().UTC()) {
		s.persistTransferLocked(state)
	}
	state.mu.Unlock()
}

func (s *Service) handleTransferDone(msg protocol.FileTransferDone) {
	state, ok := s.getTransfer(msg.TransferID)
	if !ok {
		return
	}

	if state.Direction == "download" {
		s.finishDownload(state, msg)
		return
	}

	state.mu.Lock()
	state.Status = msg.Status
	state.Message = msg.Message
	state.Size = msg.Size
	state.ChecksumSHA256 = msg.ChecksumSHA256
	state.ChecksumVerified = msg.Status == "success"
	state.CompletedAt = msg.CompletedAt
	if state.CompletedAt.IsZero() {
		state.CompletedAt = time.Now().UTC()
	}
	s.plugins.Trigger("transfer_done", state.snapshotLocked())
	s.persistTransferLocked(state)
	state.mu.Unlock()
	s.deleteTransfer(state.ID)
}

func (s *Service) finishDownload(state *transferState, msg protocol.FileTransferDone) {
	state.mu.Lock()

	if state.file != nil {
		_ = state.file.Close()
		state.file = nil
	}

	state.Message = msg.Message
	state.CompletedAt = msg.CompletedAt
	if state.CompletedAt.IsZero() {
		state.CompletedAt = time.Now().UTC()
	}
	state.Size = msg.Size
	state.ChecksumSHA256 = msg.ChecksumSHA256

	if msg.Status != "success" {
		state.Status = msg.Status
		s.cleanupTransferFilesLocked(state, false)
		snap := state.snapshotLocked()
		s.persistTransferLocked(state)
		state.mu.Unlock()
		s.plugins.Trigger("transfer_done", snap)
		s.deleteTransfer(state.ID)
		return
	}
	if err := os.Rename(state.tempPath, state.LocalPath); err != nil {
		state.Status = "failed"
		state.Message = err.Error()
		s.cleanupTransferFilesLocked(state, false)
		snap := state.snapshotLocked()
		s.persistTransferLocked(state)
		state.mu.Unlock()
		s.plugins.Trigger("transfer_done", snap)
		s.deleteTransfer(state.ID)
		return
	}
	checksum, err := common.FileSHA256(state.LocalPath)
	if err != nil {
		state.Status = "failed"
		state.Message = err.Error()
		s.cleanupTransferFilesLocked(state, true)
		snap := state.snapshotLocked()
		s.persistTransferLocked(state)
		state.mu.Unlock()
		s.plugins.Trigger("transfer_done", snap)
		s.deleteTransfer(state.ID)
		return
	}
	state.ChecksumVerified = checksum == msg.ChecksumSHA256
	if !state.ChecksumVerified {
		state.Status = "failed"
		state.Message = "checksum mismatch"
		s.cleanupTransferFilesLocked(state, true)
	} else {
		state.Status = "success"
	}
	snap := state.snapshotLocked()
	s.persistTransferLocked(state)
	state.mu.Unlock()
	s.plugins.Trigger("transfer_done", snap)
	s.deleteTransfer(state.ID)
}

func (s *Service) finishTransferWithError(state *transferState, err error) {
	state.mu.Lock()
	state.Status = "failed"
	state.Message = err.Error()
	state.CompletedAt = time.Now().UTC()
	s.cleanupTransferFilesLocked(state, false)
	snap := state.snapshotLocked()
	s.persistTransferLocked(state)
	state.mu.Unlock()
	s.plugins.Trigger("transfer_done", snap)
	s.deleteTransfer(state.ID)
}

func (s *Service) cleanupTransferFilesLocked(state *transferState, removeLocal bool) {
	if state.file != nil {
		_ = state.file.Close()
		state.file = nil
	}
	if state.tempPath != "" {
		if err := os.Remove(state.tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logger.Warn("remove temp transfer file", zap.String("transfer_id", state.ID), zap.String("path", state.tempPath), zap.Error(err))
		}
	}
	if removeLocal && state.LocalPath != "" {
		if err := os.Remove(state.LocalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.logger.Warn("remove failed transfer file", zap.String("transfer_id", state.ID), zap.String("path", state.LocalPath), zap.Error(err))
		}
	}
}

func (s *Service) putTransfer(state *transferState) {
	s.transferMu.Lock()
	defer s.transferMu.Unlock()
	s.transfers[state.ID] = state
}

func (s *Service) getTransfer(id string) (*transferState, bool) {
	s.transferMu.RLock()
	defer s.transferMu.RUnlock()
	state, ok := s.transfers[id]
	return state, ok
}

func (s *Service) deleteTransfer(id string) {
	s.transferMu.Lock()
	defer s.transferMu.Unlock()
	delete(s.transfers, id)
}

func (s *Service) transferSnapshot(id string) (TransferStatus, bool) {
	state, ok := s.getTransfer(id)
	if !ok {
		audit, ok, err := s.store.TransferAudit(id)
		if err != nil || !ok {
			return TransferStatus{}, false
		}
		return TransferStatus{
			ID:               audit.TransferID,
			AgentID:          audit.AgentID,
			Direction:        audit.Direction,
			LocalPath:        audit.LocalPath,
			RemotePath:       audit.RemotePath,
			Status:           audit.Status,
			Message:          audit.Message,
			Size:             audit.Size,
			BytesTransferred: audit.BytesTransferred,
			ChecksumSHA256:   audit.ChecksumSHA256,
			ChecksumVerified: audit.ChecksumVerified,
			CreatedAt:        audit.CreatedAt,
			CompletedAt:      audit.CompletedAt,
		}, true
	}
	return state.snapshot(), true
}

func (s *Service) clientForAgent(agentID string) (*agentConn, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	client := s.clients[agentID]
	if client == nil {
		return nil, errors.New("agent is offline")
	}
	return client, nil
}

func (s *Service) logTransferError(transferID string, err error) {
	s.logger.Warn("transfer failed", zap.String("transfer_id", transferID), zap.Error(err))
}

func (s *Service) persistTransfer(state *transferState) {
	state.mu.Lock()
	defer state.mu.Unlock()
	s.persistTransferLocked(state)
}

func (s *Service) persistTransferLocked(state *transferState) {
	if err := s.store.UpsertTransferAudit(TransferAudit{
		TransferID:       state.ID,
		AgentID:          state.AgentID,
		Direction:        state.Direction,
		LocalPath:        state.LocalPath,
		RemotePath:       state.RemotePath,
		Status:           state.Status,
		Message:          state.Message,
		Size:             state.Size,
		BytesTransferred: state.BytesTransferred,
		ChecksumSHA256:   state.ChecksumSHA256,
		ChecksumVerified: state.ChecksumVerified,
		CreatedAt:        state.CreatedAt,
		CompletedAt:      state.CompletedAt,
	}); err != nil {
		s.logger.Warn("persist transfer audit", zap.String("transfer_id", state.ID), zap.Error(err))
		return
	}
	state.lastPersistedAt = time.Now().UTC()
	state.lastPersistedBytes = state.BytesTransferred
}

func (s *Service) shouldPersistTransferProgressLocked(state *transferState, now time.Time) bool {
	if state.lastPersistedAt.IsZero() {
		return true
	}
	if state.BytesTransferred-state.lastPersistedBytes >= transferAuditMinBytes {
		return true
	}
	return now.Sub(state.lastPersistedAt) >= transferAuditMinInterval
}
