package common

import (
	"bytes"
	"errors"
	"testing"
)

func TestChunkAssemblerWritesInOrder(t *testing.T) {
	var buf bytes.Buffer
	a := NewChunkAssembler(0)

	steps := []struct {
		seq  int
		data string
	}{
		{0, "a"},
		{1, "b"},
		{2, "c"},
	}
	for _, s := range steps {
		if err := a.Write(&buf, s.seq, []byte(s.data)); err != nil {
			t.Fatalf("write seq %d: %v", s.seq, err)
		}
	}

	if got := buf.String(); got != "abc" {
		t.Fatalf("unexpected output: %q", got)
	}
	if err := a.Finish(3); err != nil {
		t.Fatalf("finish: %v", err)
	}
}

func TestChunkAssemblerReordersOutOfOrder(t *testing.T) {
	var buf bytes.Buffer
	a := NewChunkAssembler(0)

	for _, s := range []struct {
		seq  int
		data string
	}{
		{1, "b"},
		{2, "c"},
		{0, "a"},
	} {
		if err := a.Write(&buf, s.seq, []byte(s.data)); err != nil {
			t.Fatalf("write seq %d: %v", s.seq, err)
		}
	}

	if got := buf.String(); got != "abc" {
		t.Fatalf("unexpected output: %q", got)
	}
	if err := a.Finish(3); err != nil {
		t.Fatalf("finish: %v", err)
	}
}

func TestChunkAssemblerRejectsDuplicate(t *testing.T) {
	var buf bytes.Buffer
	a := NewChunkAssembler(0)

	if err := a.Write(&buf, 0, []byte("a")); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := a.Write(&buf, 0, []byte("a")); !errors.Is(err, ErrChunkDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestChunkAssemblerRejectsDuplicateOfBuffered(t *testing.T) {
	var buf bytes.Buffer
	a := NewChunkAssembler(0)

	if err := a.Write(&buf, 1, []byte("b")); err != nil {
		t.Fatalf("write buffered: %v", err)
	}
	if err := a.Write(&buf, 1, []byte("b")); !errors.Is(err, ErrChunkDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestChunkAssemblerDetectsMissingChunk(t *testing.T) {
	var buf bytes.Buffer
	a := NewChunkAssembler(0)

	for _, s := range []struct {
		seq  int
		data string
	}{
		{0, "a"},
		{2, "c"},
	} {
		if err := a.Write(&buf, s.seq, []byte(s.data)); err != nil {
			t.Fatalf("write seq %d: %v", s.seq, err)
		}
	}

	if got := buf.String(); got != "a" {
		t.Fatalf("only contiguous prefix should be flushed, got %q", got)
	}
	if err := a.Finish(3); err == nil {
		t.Fatalf("expected missing chunk error")
	}
}

func TestChunkAssemblerDetectsShortTransfer(t *testing.T) {
	var buf bytes.Buffer
	a := NewChunkAssembler(0)

	if err := a.Write(&buf, 0, []byte("a")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := a.Finish(3); err == nil {
		t.Fatalf("expected short transfer error")
	}
}

func TestChunkAssemblerRejectsExcessBuffering(t *testing.T) {
	var buf bytes.Buffer
	a := NewChunkAssembler(2)

	// seq 3 and 4 buffer two chunks; a third gap must fail.
	if err := a.Write(&buf, 3, []byte("d")); err != nil {
		t.Fatalf("write seq 3: %v", err)
	}
	if err := a.Write(&buf, 4, []byte("e")); err != nil {
		t.Fatalf("write seq 4: %v", err)
	}
	if err := a.Write(&buf, 5, []byte("f")); !errors.Is(err, ErrChunkOutOfOrder) {
		t.Fatalf("expected out-of-order error, got %v", err)
	}
}

func TestChunkAssemblerResume(t *testing.T) {
	var buf bytes.Buffer
	a := NewChunkAssembler(0)
	a.Resume(2)

	if err := a.Write(&buf, 2, []byte("c")); err != nil {
		t.Fatalf("write seq 2: %v", err)
	}
	if err := a.Write(&buf, 3, []byte("d")); err != nil {
		t.Fatalf("write seq 3: %v", err)
	}
	if got := buf.String(); got != "cd" {
		t.Fatalf("unexpected output: %q", got)
	}
	if err := a.Finish(4); err != nil {
		t.Fatalf("finish: %v", err)
	}
}

func TestChunkCount(t *testing.T) {
	cases := []struct {
		size      int64
		chunkSize int
		want      int
	}{
		{0, 256, 0},
		{10, 0, 0},
		{100, 10, 10},
		{101, 10, 11},
		{1, 256, 1},
	}
	for _, c := range cases {
		if got := ChunkCount(c.size, c.chunkSize); got != c.want {
			t.Fatalf("ChunkCount(%d, %d) = %d, want %d", c.size, c.chunkSize, got, c.want)
		}
	}
}
