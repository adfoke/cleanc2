package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"cleanc2/internal/common"
	"cleanc2/internal/protocol"
)

type Client struct {
	cfg       Config
	logger    *zap.Logger
	host      common.HostInfo
	agentID   string
	startedAt time.Time
	dialer    *websocket.Dialer
	writeMu   sync.Mutex
	taskMu    sync.Mutex
	running   map[string]context.CancelFunc
	resultMu  sync.Mutex
	results   map[string]protocol.TaskResult
	uploadMu  sync.Mutex
	uploads   map[string]*uploadState
}

type uploadState struct {
	remotePath string
	tempPath   string
	file       *os.File
	size       int64
	received   int64
}

func New(cfg Config, logger *zap.Logger) (*Client, error) {
	if cfg.ServerURL == "" {
		return nil, errors.New("server url is required")
	}
	if cfg.Token == "" {
		return nil, errors.New("token is required")
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}

	host := common.CollectHostInfo()
	agentID := cfg.AgentID
	if agentID == "" {
		agentID = host.Hostname
		if agentID == "" {
			agentID = common.NewID()
		}
	}

	tlsCfg, err := buildClientTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{
		cfg:       cfg,
		logger:    logger,
		host:      host,
		agentID:   agentID,
		startedAt: time.Now(),
		dialer: &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 10 * time.Second,
			TLSClientConfig:  tlsCfg,
		},
		running: make(map[string]context.CancelFunc),
		results: make(map[string]protocol.TaskResult),
		uploads: make(map[string]*uploadState),
	}, nil
}

func (c *Client) Run(ctx context.Context) error {
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			c.logger.Warn("agent session ended", zap.Error(err))
		}

		sleep := backoff + time.Duration(rand.IntN(500))*time.Millisecond
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		backoff *= 2
		if backoff > c.cfg.MaxBackoff {
			backoff = c.cfg.MaxBackoff
		}
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	conn, _, err := c.dialer.DialContext(ctx, c.cfg.ServerURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := c.send(conn, protocol.TypeHello, protocol.AgentHello{
		AgentID:     c.agentID,
		Token:       c.cfg.Token,
		Hostname:    c.host.Hostname,
		OS:          c.host.OS,
		Arch:        c.host.Arch,
		IPAddrs:     c.host.IPAddrs,
		Tags:        c.cfg.Tags,
		Fingerprint: c.host.Fingerprint,
		Version:     "v0.3.0",
		ConnectedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}

	heartbeatDone := make(chan struct{})
	go c.heartbeatLoop(ctx, conn, heartbeatDone)
	defer close(heartbeatDone)

	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			return err
		}

		switch env.Type {
		case protocol.TypeHelloAck:
			ack, err := protocol.UnmarshalPayload[protocol.HelloAck](env)
			if err != nil {
				return err
			}
			c.logger.Info("connected", zap.String("agent_id", ack.AgentID), zap.Int("pending", len(ack.PendingTasks)))
		case protocol.TypeTaskDispatch:
			task, err := protocol.UnmarshalPayload[protocol.Task](env)
			if err != nil {
				return err
			}
			if err := c.ackTask(conn, task.ID); err != nil {
				return err
			}
			if result, ok := c.cachedResult(task.ID); ok {
				if err := c.send(conn, protocol.TypeTaskResult, result); err != nil {
					return err
				}
				continue
			}
			if c.taskRunning(task.ID) {
				continue
			}
			c.startTask(ctx, conn, task)
		case protocol.TypeTaskCancel:
			cancelMsg, err := protocol.UnmarshalPayload[protocol.TaskCancel](env)
			if err != nil {
				return err
			}
			c.cancelTask(cancelMsg.TaskID)
		case protocol.TypeFileTransferStart:
			start, err := protocol.UnmarshalPayload[protocol.FileTransferStart](env)
			if err != nil {
				return err
			}
			c.handleTransferStart(ctx, conn, start)
		case protocol.TypeFileTransferChunk:
			chunk, err := protocol.UnmarshalPayload[protocol.FileTransferChunk](env)
			if err != nil {
				return err
			}
			c.handleTransferChunk(conn, chunk)
		case protocol.TypeFileTransferDone:
			done, err := protocol.UnmarshalPayload[protocol.FileTransferDone](env)
			if err != nil {
				return err
			}
			c.handleTransferDone(conn, done)
		case protocol.TypeError:
			msg, err := protocol.UnmarshalPayload[protocol.ErrorMessage](env)
			if err != nil {
				return err
			}
			return fmt.Errorf("%s: %s", msg.Code, msg.Message)
		}
	}
}

func (c *Client) heartbeatLoop(ctx context.Context, conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			_ = c.send(conn, protocol.TypeHeartbeat, protocol.Heartbeat{
				AgentID:   c.agentID,
				Timestamp: time.Now().UTC(),
			})
			if metrics, err := c.collectMetrics(); err == nil {
				_ = c.send(conn, protocol.TypeMetricsReport, metrics)
			}
		}
	}
}

func (c *Client) startTask(ctx context.Context, conn *websocket.Conn, task protocol.Task) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSecs)*time.Second)

	c.taskMu.Lock()
	c.running[task.ID] = cancel
	c.taskMu.Unlock()

	go func() {
		defer cancel()
		defer c.unregisterTask(task.ID)

		start := time.Now()
		cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", task.Command)

		var stdoutBuilder strings.Builder
		var stderrBuilder strings.Builder
		cmd.Stdout = &stdoutBuilder
		cmd.Stderr = &stderrBuilder

		err := cmd.Run()
		result := protocol.TaskResult{
			TaskID:      task.ID,
			AgentID:     c.agentID,
			Status:      "success",
			CompletedAt: time.Now().UTC(),
			DurationMS:  time.Since(start).Milliseconds(),
			Stdout:      stdoutBuilder.String(),
			Stderr:      stderrBuilder.String(),
		}

		if err != nil {
			result.Status = "failed"
			switch runCtx.Err() {
			case context.DeadlineExceeded:
				result.Status = "timeout"
			case context.Canceled:
				result.Status = "canceled"
			}
			if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
				result.ExitCode = exitErr.ExitCode()
			} else if result.Status == "failed" {
				result.Stderr = strings.TrimSpace(result.Stderr + "\n" + err.Error())
			}
		}

		c.cacheResult(result)
		if err := c.send(conn, protocol.TypeTaskResult, result); err != nil {
			c.logger.Warn("send result", zap.String("task_id", task.ID), zap.Error(err))
		}
	}()
}

func (c *Client) cancelTask(taskID string) {
	c.taskMu.Lock()
	cancel := c.running[taskID]
	c.taskMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Client) unregisterTask(taskID string) {
	c.taskMu.Lock()
	defer c.taskMu.Unlock()
	delete(c.running, taskID)
}

func (c *Client) taskRunning(taskID string) bool {
	c.taskMu.Lock()
	defer c.taskMu.Unlock()
	_, ok := c.running[taskID]
	return ok
}

func (c *Client) cacheResult(result protocol.TaskResult) {
	c.resultMu.Lock()
	defer c.resultMu.Unlock()
	c.results[result.TaskID] = result
}

func (c *Client) cachedResult(taskID string) (protocol.TaskResult, bool) {
	c.resultMu.Lock()
	defer c.resultMu.Unlock()
	result, ok := c.results[taskID]
	return result, ok
}

func (c *Client) ackTask(conn *websocket.Conn, taskID string) error {
	return c.send(conn, protocol.TypeTaskAck, protocol.TaskAck{
		TaskID:     taskID,
		AgentID:    c.agentID,
		ReceivedAt: time.Now().UTC(),
	})
}

func (c *Client) handleTransferStart(ctx context.Context, conn *websocket.Conn, start protocol.FileTransferStart) {
	switch start.Direction {
	case "upload":
		if err := os.MkdirAll(filepath.Dir(start.RemotePath), 0o755); err != nil {
			c.sendTransferDone(conn, protocol.FileTransferDone{
				TransferID:  start.TransferID,
				AgentID:     c.agentID,
				Direction:   start.Direction,
				Status:      "failed",
				Message:     err.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return
		}

		tempPath := start.RemotePath + ".part." + start.TransferID
		file, err := os.Create(tempPath)
		if err != nil {
			c.sendTransferDone(conn, protocol.FileTransferDone{
				TransferID:  start.TransferID,
				AgentID:     c.agentID,
				Direction:   start.Direction,
				Status:      "failed",
				Message:     err.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return
		}

		c.uploadMu.Lock()
		c.uploads[start.TransferID] = &uploadState{
			remotePath: start.RemotePath,
			tempPath:   tempPath,
			file:       file,
			size:       start.Size,
		}
		c.uploadMu.Unlock()
	case "download":
		go c.sendFile(conn, start)
	}
}

func (c *Client) handleTransferChunk(conn *websocket.Conn, chunk protocol.FileTransferChunk) {
	c.uploadMu.Lock()
	state := c.uploads[chunk.TransferID]
	c.uploadMu.Unlock()
	if state == nil {
		return
	}

	data, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		c.failUpload(conn, chunk.TransferID, state, err)
		return
	}
	if _, err := state.file.Write(data); err != nil {
		c.failUpload(conn, chunk.TransferID, state, err)
		return
	}
	state.received += int64(len(data))
}

func (c *Client) handleTransferDone(conn *websocket.Conn, done protocol.FileTransferDone) {
	if done.Direction != "upload" {
		return
	}

	c.uploadMu.Lock()
	state := c.uploads[done.TransferID]
	delete(c.uploads, done.TransferID)
	c.uploadMu.Unlock()
	if state == nil {
		return
	}

	_ = state.file.Close()
	status := protocol.FileTransferDone{
		TransferID:  done.TransferID,
		AgentID:     c.agentID,
		Direction:   done.Direction,
		Status:      "success",
		Size:        state.received,
		CompletedAt: time.Now().UTC(),
	}

	if done.Status != "complete" {
		status.Status = "failed"
		status.Message = done.Message
	} else if err := os.Rename(state.tempPath, state.remotePath); err != nil {
		status.Status = "failed"
		status.Message = err.Error()
	} else if checksum, err := common.FileSHA256(state.remotePath); err != nil {
		status.Status = "failed"
		status.Message = err.Error()
	} else {
		status.ChecksumSHA256 = checksum
		if done.ChecksumSHA256 != "" && checksum != done.ChecksumSHA256 {
			status.Status = "failed"
			status.Message = "checksum mismatch"
		}
	}

	c.sendTransferDone(conn, status)
}

func (c *Client) sendFile(conn *websocket.Conn, start protocol.FileTransferStart) {
	file, err := os.Open(start.RemotePath)
	if err != nil {
		c.sendTransferDone(conn, protocol.FileTransferDone{
			TransferID:  start.TransferID,
			AgentID:     c.agentID,
			Direction:   start.Direction,
			Status:      "failed",
			Message:     err.Error(),
			CompletedAt: time.Now().UTC(),
		})
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.sendTransferDone(conn, protocol.FileTransferDone{
			TransferID:  start.TransferID,
			AgentID:     c.agentID,
			Direction:   start.Direction,
			Status:      "failed",
			Message:     err.Error(),
			CompletedAt: time.Now().UTC(),
		})
		return
	}

	chunkSize := start.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 256 * 1024
	}

	checksum, err := common.FileSHA256(start.RemotePath)
	if err != nil {
		c.sendTransferDone(conn, protocol.FileTransferDone{
			TransferID:  start.TransferID,
			AgentID:     c.agentID,
			Direction:   start.Direction,
			Status:      "failed",
			Message:     err.Error(),
			CompletedAt: time.Now().UTC(),
		})
		return
	}

	if err := c.send(conn, protocol.TypeFileTransferStart, protocol.FileTransferStart{
		TransferID:     start.TransferID,
		AgentID:        c.agentID,
		Direction:      start.Direction,
		LocalPath:      start.LocalPath,
		RemotePath:     start.RemotePath,
		Size:           info.Size(),
		ChunkSize:      chunkSize,
		ChecksumSHA256: checksum,
		RequestedAt:    time.Now().UTC(),
	}); err != nil {
		c.logger.Warn("send download start", zap.String("transfer_id", start.TransferID), zap.Error(err))
		return
	}

	buf := make([]byte, chunkSize)
	seq := 0
	var transferred int64
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if err := c.send(conn, protocol.TypeFileTransferChunk, protocol.FileTransferChunk{
				TransferID: start.TransferID,
				Seq:        seq,
				Data:       base64.StdEncoding.EncodeToString(buf[:n]),
			}); err != nil {
				c.logger.Warn("send download chunk", zap.String("transfer_id", start.TransferID), zap.Error(err))
				return
			}
			transferred += int64(n)
			seq++
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			c.sendTransferDone(conn, protocol.FileTransferDone{
				TransferID:  start.TransferID,
				AgentID:     c.agentID,
				Direction:   start.Direction,
				Status:      "failed",
				Message:     readErr.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return
		}
	}

	c.sendTransferDone(conn, protocol.FileTransferDone{
		TransferID:     start.TransferID,
		AgentID:        c.agentID,
		Direction:      start.Direction,
		Status:         "success",
		Size:           transferred,
		ChecksumSHA256: checksum,
		CompletedAt:    time.Now().UTC(),
	})
}

func (c *Client) sendTransferDone(conn *websocket.Conn, done protocol.FileTransferDone) {
	if err := c.send(conn, protocol.TypeFileTransferDone, done); err != nil {
		c.logger.Warn("send transfer done", zap.String("transfer_id", done.TransferID), zap.Error(err))
	}
}

func (c *Client) failUpload(conn *websocket.Conn, transferID string, state *uploadState, err error) {
	c.uploadMu.Lock()
	if current := c.uploads[transferID]; current == state {
		delete(c.uploads, transferID)
	}
	c.uploadMu.Unlock()
	if state.file != nil {
		_ = state.file.Close()
	}
	if state.tempPath != "" {
		_ = os.Remove(state.tempPath)
	}
	c.sendTransferDone(conn, protocol.FileTransferDone{
		TransferID:  transferID,
		AgentID:     c.agentID,
		Direction:   "upload",
		Status:      "failed",
		Message:     err.Error(),
		Size:        state.received,
		CompletedAt: time.Now().UTC(),
	})
}

func (c *Client) send(conn *websocket.Conn, msgType string, payload any) error {
	msg, err := protocol.MarshalMessage(msgType, payload)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, msg)
}

func buildClientTLSConfig(cfg Config) (*tls.Config, error) {
	if !strings.HasPrefix(cfg.ServerURL, "wss://") {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: cfg.ServerName,
	}

	if cfg.CACertFile != "" {
		caPEM, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("read ca cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("append ca cert failed")
		}
		tlsCfg.RootCAs = pool
	}

	if cfg.ClientCertFile != "" || cfg.ClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client key pair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

func decodeEnvelope(raw []byte) (protocol.Envelope, error) {
	var env protocol.Envelope
	err := json.Unmarshal(raw, &env)
	return env, err
}
