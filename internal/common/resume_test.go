package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResumeOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.part")

	off, err := ResumeOffset(path, 256)
	if err != nil {
		t.Fatalf("resume offset missing file: %v", err)
	}
	if off != 0 {
		t.Fatalf("expected 0 for missing file, got %d", off)
	}

	if err := os.WriteFile(path, make([]byte, 1000), 0o600); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	off, err = ResumeOffset(path, 256)
	if err != nil {
		t.Fatalf("resume offset: %v", err)
	}
	if off != 768 {
		t.Fatalf("expected 768, got %d", off)
	}
}

func TestOpenPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.part")

	f, err := OpenPartialFile(path, 0)
	if err != nil {
		t.Fatalf("open fresh: %v", err)
	}
	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatalf("write fresh: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close fresh: %v", err)
	}

	f, err = OpenPartialFile(path, 5)
	if err != nil {
		t.Fatalf("open resume: %v", err)
	}
	if _, err := f.Write([]byte(" world")); err != nil {
		t.Fatalf("write resume: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close resume: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("unexpected content: %q", string(got))
	}
}
