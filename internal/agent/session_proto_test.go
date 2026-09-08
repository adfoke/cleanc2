package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"coc2/internal/protocol"
)

// fakeServer speaks the server side of one agent session: it verifies the
// hello framing, replies hello_ack in protobuf, dispatches one task, and
// reports every frame opcode it received back to the test.
type fakeServer struct {
	t        *testing.T
	upgraded chan struct{} // closed when hello_ack has been sent binary
	ackSeen  chan int      // opcode of the task_ack frame
	resultCh chan protocol.TaskResult
	gotHello chan protocol.AgentHello
}

// TestAgentSessionNegotiatesProtobuf drives the real Client through a full
// session against a protobuf-speaking fake server: hello must be JSON with
// ProtoVersion set (the handshake is always legacy), every frame after the
// binary hello_ack must come back as protobuf — including a real /bin/sh
// execution result.
func TestAgentSessionNegotiatesProtobuf(t *testing.T) {
	fs := &fakeServer{
		t:        t,
		upgraded: make(chan struct{}),
		ackSeen:  make(chan int, 1),
		resultCh: make(chan protocol.TaskResult, 1),
		gotHello: make(chan protocol.AgentHello, 1),
	}

	up := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		fs.serve(conn)
	}))
	defer server.Close()

	wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://") + "/ws/agent"
	client, err := New(Config{
		ServerURL:         wsURL,
		Token:             "tok",
		AgentID:           "agent-e2e",
		HeartbeatInterval: time.Hour, // silence the ticker for the test window
		MaxBackoff:        time.Hour,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sessionDone := make(chan error, 1)
	go func() { sessionDone <- client.runOnce(ctx) }()

	select {
	case hello := <-fs.gotHello:
		if hello.ProtoVersion != protocol.BinaryWireVersion {
			t.Fatalf("hello proto_version = %d, want %d", hello.ProtoVersion, protocol.BinaryWireVersion)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("hello never arrived")
	}

	// Fake server waits for the protobuf task_ack after sending binary ack.
	select {
	case opcode := <-fs.ackSeen:
		if opcode != websocket.BinaryMessage {
			t.Fatalf("task_ack opcode = %d, want binary after protobuf hello_ack", opcode)
		}
	case err := <-sessionDone:
		t.Fatalf("session died before task_ack: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("task_ack never arrived")
	}

	select {
	case result := <-fs.resultCh:
		if result.TaskID != "t-e2e" || result.Status != "success" || result.ExitCode != 0 {
			t.Fatalf("unexpected task result: %+v", result)
		}
		if !strings.Contains(result.Stdout, "proto-session") {
			t.Fatalf("unexpected stdout: %q", result.Stdout)
		}
	case err := <-sessionDone:
		t.Fatalf("session died before result: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatalf("task result never arrived")
	}
	cancel()
}

func (fs *fakeServer) serve(conn *websocket.Conn) {
	// 1. hello must arrive as legacy JSON text with ProtoVersion advertised.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	opcode, raw, err := conn.ReadMessage()
	if err != nil {
		fs.t.Errorf("read hello: %v", err)
		return
	}
	if opcode != websocket.TextMessage {
		fs.t.Errorf("hello opcode = %d, want text (handshake is always legacy)", opcode)
		return
	}
	in, err := protocol.DecodeFrame(opcode, raw)
	if err != nil {
		fs.t.Errorf("decode hello: %v", err)
		return
	}
	hello, err := protocol.PayloadOf[protocol.AgentHello](in)
	if err != nil {
		fs.t.Errorf("hello payload: %v", err)
		return
	}
	fs.gotHello <- hello

	// 2. answer hello_ack as protobuf binary.
	ack, err := protocol.MarshalBinaryEnvelope(protocol.TypeHelloAck, protocol.HelloAck{
		ServerTime: time.Now().UTC(), AgentID: hello.AgentID,
	})
	if err != nil {
		fs.t.Errorf("marshal ack: %v", err)
		return
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, ack); err != nil {
		fs.t.Errorf("write hello_ack: %v", err)
		return
	}
	close(fs.upgraded)

	// 3. dispatch one real shell task, also binary.
	task, err := protocol.MarshalBinaryEnvelope(protocol.TypeTaskDispatch, protocol.Task{
		ID: "t-e2e", AgentID: hello.AgentID, Type: "shell",
		Command: "echo proto-session", TimeoutSecs: 10, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		fs.t.Errorf("marshal task: %v", err)
		return
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, task); err != nil {
		fs.t.Errorf("write task: %v", err)
		return
	}

	// 4. collect the agent's replies until the protobuf task_result lands.
	for {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		opcode, raw, err := conn.ReadMessage()
		if err != nil {
			return // session cancelled or client gone
		}
		in, err := protocol.DecodeFrame(opcode, raw)
		if err != nil {
			fs.t.Errorf("decode inbound: %v", err)
			return
		}
		switch in.MsgType {
		case protocol.TypeTaskAck:
			select {
			case fs.ackSeen <- opcode:
			default:
			}
		case protocol.TypeTaskResult:
			result, err := protocol.PayloadOf[protocol.TaskResult](in)
			if err != nil {
				fs.t.Errorf("decode result: %v", err)
				return
			}
			fs.resultCh <- result
			return
		}
	}
}

// TestAgentLegacyServerStaysJSON: against a server that answers hello_ack as
// JSON text (pre-S2 behaviour), the client must keep sending JSON frames.
func TestAgentLegacyServerStaysJSON(t *testing.T) {
	sawLegacyResult := make(chan bool, 1)
	up := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		opcode, raw, err := conn.ReadMessage()
		if err != nil || opcode != websocket.TextMessage {
			t.Errorf("legacy hello must be text: opcode=%d err=%v", opcode, err)
			return
		}
		in, _ := protocol.DecodeFrame(opcode, raw)
		hello, _ := protocol.PayloadOf[protocol.AgentHello](in)

		// Legacy JSON ack.
		envRaw, _ := json.Marshal(protocol.HelloAck{ServerTime: time.Now().UTC(), AgentID: hello.AgentID})
		frame, _ := json.Marshal(protocol.Envelope{Type: protocol.TypeHelloAck, Payload: envRaw})
		if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			return
		}

		// A binary dispatch afterwards must still be understood (dual-stack
		// reads) — and the agent's reply must stay JSON (legacy echo).
		task, _ := protocol.MarshalBinaryEnvelope(protocol.TypeTaskDispatch, protocol.Task{
			ID: "t-legacy", AgentID: hello.AgentID, Type: "shell",
			Command: "echo legacy", TimeoutSecs: 10, CreatedAt: time.Now().UTC(),
		})
		if err := conn.WriteMessage(websocket.BinaryMessage, task); err != nil {
			return
		}
		for {
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			opcode, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			in, err := protocol.DecodeFrame(opcode, raw)
			if err != nil {
				return
			}
			if in.MsgType == protocol.TypeTaskResult {
				sawLegacyResult <- opcode == websocket.TextMessage
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://") + "/ws/agent"
	client, err := New(Config{
		ServerURL: wsURL, Token: "tok", AgentID: "agent-legacy",
		HeartbeatInterval: time.Hour, MaxBackoff: time.Hour,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.runOnce(ctx)

	select {
	case ok := <-sawLegacyResult:
		if !ok {
			t.Fatalf("legacy server received a binary frame from the agent")
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no task_result from agent")
	}
}
