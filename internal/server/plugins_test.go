package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestPluginManagerTrigger(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	pluginPath := filepath.Join(dir, "echo-hook.sh")
	script := "#!/bin/sh\ncat > \"$PWD/out.txt\"\n"
	if err := os.WriteFile(pluginPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write plugin: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	pm, err := NewPluginManager(dir, zap.NewNop())
	if err != nil {
		t.Fatalf("new plugin manager: %v", err)
	}
	pm.Trigger("task_result", map[string]string{"task_id": "t1"})

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	for {
		raw, err = os.ReadFile(outPath)
		if err == nil && len(raw) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("read plugin output: %v raw=%q", err, string(raw))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
