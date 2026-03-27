package server

import (
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"cleanc2/internal/protocol"
)

type outboundMessage struct {
	data []byte
}

func (a *agentConn) readLoop() {
	defer a.service.unregister(a)

	a.conn.SetReadLimit(16 << 20)
	a.conn.SetReadDeadline(time.Now().Add(a.service.cfg.PongWait))
	a.conn.SetPongHandler(func(string) error {
		a.conn.SetReadDeadline(time.Now().Add(a.service.cfg.PongWait))
		if a.id != "" {
			a.service.touch(a.id)
		}
		return nil
	})

	for {
		var env protocol.Envelope
		if err := a.conn.ReadJSON(&env); err != nil {
			a.service.logger.Info("agent disconnected", zap.String("agent_id", a.id), zap.Error(err))
			return
		}

		switch env.Type {
		case protocol.TypeHello:
			hello, err := protocol.UnmarshalPayload[protocol.AgentHello](env)
			if err != nil {
				a.sendProtocolError("bad_hello", err.Error())
				continue
			}
			if hello.Token != a.service.cfg.AuthToken {
				a.sendProtocolError("auth_failed", "token mismatch")
				return
			}

			pending, err := a.service.register(a, hello)
			if err != nil {
				a.sendProtocolError("register_failed", err.Error())
				return
			}

			ack := protocol.HelloAck{
				ServerTime:   time.Now().UTC(),
				AgentID:      hello.AgentID,
				PendingTasks: pending,
			}
			if err := a.sendMessage(protocol.TypeHelloAck, ack); err != nil {
				a.requeueTasks(pending)
				return
			}
			for i, task := range pending {
				if err := a.sendTask(task); err != nil {
					a.requeueTasks(pending[i:])
					return
				}
			}
			a.service.plugins.Trigger("agent_connected", hello)
			a.service.logger.Info("agent connected", zap.String("agent_id", hello.AgentID), zap.String("hostname", hello.Hostname))
		case protocol.TypeHeartbeat:
			if a.id == "" {
				a.sendProtocolError("not_registered", "hello is required first")
				continue
			}
			if _, err := protocol.UnmarshalPayload[protocol.Heartbeat](env); err != nil {
				a.sendProtocolError("bad_heartbeat", err.Error())
				continue
			}
			a.service.touch(a.id)
		case protocol.TypeTaskResult:
			result, err := protocol.UnmarshalPayload[protocol.TaskResult](env)
			if err != nil {
				a.sendProtocolError("bad_result", err.Error())
				continue
			}
			if err := a.service.store.SaveResult(result); err != nil {
				a.service.logger.Warn("save result", zap.String("task_id", result.TaskID), zap.Error(err))
				continue
			}
			a.service.plugins.Trigger("task_result", result)
			a.service.touch(result.AgentID)
		case protocol.TypeTaskAck:
			ack, err := protocol.UnmarshalPayload[protocol.TaskAck](env)
			if err != nil {
				a.sendProtocolError("bad_task_ack", err.Error())
				continue
			}
			if err := a.service.store.MarkDispatched(ack.TaskID); err != nil {
				a.service.logger.Warn("mark dispatched", zap.String("task_id", ack.TaskID), zap.Error(err))
			}
		case protocol.TypeMetricsReport:
			report, err := protocol.UnmarshalPayload[protocol.MetricsReport](env)
			if err != nil {
				a.sendProtocolError("bad_metrics_report", err.Error())
				continue
			}
			a.service.handleMetricsReport(report)
		case protocol.TypeFileTransferStart:
			start, err := protocol.UnmarshalPayload[protocol.FileTransferStart](env)
			if err != nil {
				a.sendProtocolError("bad_transfer_start", err.Error())
				continue
			}
			a.service.handleTransferStart(start)
		case protocol.TypeFileTransferChunk:
			chunk, err := protocol.UnmarshalPayload[protocol.FileTransferChunk](env)
			if err != nil {
				a.sendProtocolError("bad_transfer_chunk", err.Error())
				continue
			}
			a.service.handleTransferChunk(chunk)
		case protocol.TypeFileTransferDone:
			done, err := protocol.UnmarshalPayload[protocol.FileTransferDone](env)
			if err != nil {
				a.sendProtocolError("bad_transfer_done", err.Error())
				continue
			}
			a.service.handleTransferDone(done)
		default:
			a.sendProtocolError("unsupported_type", env.Type)
		}
	}
}

func (a *agentConn) writeLoop() {
	ticker := time.NewTicker(a.service.cfg.PingPeriod)
	defer func() {
		ticker.Stop()
		a.close()
	}()

	for {
		select {
		case msg, ok := <-a.send:
			a.conn.SetWriteDeadline(time.Now().Add(a.service.cfg.WriteWait))
			if !ok {
				_ = a.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := a.conn.WriteMessage(websocket.TextMessage, msg.data); err != nil {
				return
			}
		case <-ticker.C:
			a.conn.SetWriteDeadline(time.Now().Add(a.service.cfg.WriteWait))
			if err := a.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (a *agentConn) sendTask(task protocol.Task) error {
	return a.sendMessage(protocol.TypeTaskDispatch, task)
}

func (a *agentConn) sendProtocolError(code, message string) {
	_ = a.sendMessage(protocol.TypeError, protocol.ErrorMessage{
		Code:    code,
		Message: message,
	})
}

func (a *agentConn) sendMessage(msgType string, payload any) error {
	msg, err := protocol.MarshalMessage(msgType, payload)
	if err != nil {
		return err
	}

	select {
	case a.send <- outboundMessage{data: msg}:
		return nil
	default:
		return websocket.ErrCloseSent
	}
}

func (a *agentConn) close() {
	a.closeOnce.Do(func() {
		close(a.send)
		_ = a.conn.Close()
	})
}

func (a *agentConn) requeueTasks(tasks []protocol.Task) {
	for _, task := range tasks {
		if err := a.service.store.AddTask(task); err != nil {
			a.service.logger.Warn("requeue task", zap.String("task_id", task.ID), zap.Error(err))
		}
	}
}

func decodeEnvelope(raw []byte) (protocol.Envelope, error) {
	var env protocol.Envelope
	err := json.Unmarshal(raw, &env)
	return env, err
}
