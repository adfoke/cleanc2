package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMarshalAndUnmarshalEnvelope(t *testing.T) {
	now := time.Now().UTC().Round(time.Second)
	raw, err := MarshalMessage(TypeTaskDispatch, Task{
		ID:          "task-1",
		AgentID:     "agent-1",
		Type:        "shell",
		Command:     "echo ok",
		TimeoutSecs: 5,
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Type != TypeTaskDispatch {
		t.Fatalf("unexpected type: %s", env.Type)
	}

	task, err := UnmarshalPayload[Task](env)
	if err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	if task.ID != "task-1" || task.Command != "echo ok" {
		t.Fatalf("unexpected task: %+v", task)
	}
}
