package common

import (
	"errors"
	"fmt"
	"io"
)

var (
	// ErrChunkDuplicate indicates a chunk with an already-seen sequence number.
	ErrChunkDuplicate = errors.New("duplicate chunk sequence")
	// ErrChunkOutOfOrder indicates too many out-of-order chunks were buffered
	// without the next expected sequence arriving.
	ErrChunkOutOfOrder = errors.New("too many out-of-order chunks buffered")
)

// DefaultMaxPendingChunks bounds how many out-of-order chunks may be buffered
// before a transfer is considered broken.
const DefaultMaxPendingChunks = 256

// ChunkAssembler reorders file-transfer chunks by their Seq field, writing them
// to the destination in order. It detects duplicates and gaps, so a transfer no
// longer relies solely on the final SHA256 checksum for integrity.
type ChunkAssembler struct {
	nextSeq    int
	pending    map[int][]byte
	maxPending int
}

// NewChunkAssembler returns an assembler that buffers up to maxPending
// out-of-order chunks. A non-positive maxPending falls back to
// DefaultMaxPendingChunks.
func NewChunkAssembler(maxPending int) *ChunkAssembler {
	if maxPending <= 0 {
		maxPending = DefaultMaxPendingChunks
	}
	return &ChunkAssembler{
		pending:    make(map[int][]byte),
		maxPending: maxPending,
	}
}

// Write writes data for seq to w in ascending order. If seq is already received
// (duplicate) it returns ErrChunkDuplicate; if seq is ahead of the expected
// sequence it is buffered until the gap is filled. Buffering more than
// maxPending chunks returns ErrChunkOutOfOrder.
func (a *ChunkAssembler) Write(w io.Writer, seq int, data []byte) error {
	if seq < 0 {
		return fmt.Errorf("invalid chunk sequence %d", seq)
	}
	if seq < a.nextSeq {
		return fmt.Errorf("%w: seq %d already received (next expected %d)", ErrChunkDuplicate, seq, a.nextSeq)
	}
	if _, exists := a.pending[seq]; exists {
		return fmt.Errorf("%w: seq %d", ErrChunkDuplicate, seq)
	}

	if seq == a.nextSeq {
		if _, err := w.Write(data); err != nil {
			return err
		}
		a.nextSeq++
		return a.flush(w)
	}

	if len(a.pending) >= a.maxPending {
		return fmt.Errorf("%w: seq %d buffered while expecting %d", ErrChunkOutOfOrder, seq, a.nextSeq)
	}
	a.pending[seq] = append([]byte(nil), data...)
	return nil
}

func (a *ChunkAssembler) flush(w io.Writer) error {
	for {
		data, ok := a.pending[a.nextSeq]
		if !ok {
			return nil
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		delete(a.pending, a.nextSeq)
		a.nextSeq++
	}
}

// Pending returns the number of buffered, not-yet-written chunks.
func (a *ChunkAssembler) Pending() int {
	return len(a.pending)
}

// Resume sets the next expected sequence number, used when resuming a transfer
// whose earlier chunks are already persisted on disk.
func (a *ChunkAssembler) Resume(nextSeq int) {
	a.nextSeq = nextSeq
}

// Finish verifies that every chunk in [0, totalChunks) was received
// contiguously. It reports buffered chunks (a gap never filled) and short
// transfers. A non-positive totalChunks skips the count check, which is useful
// when the total size is not yet known.
func (a *ChunkAssembler) Finish(totalChunks int) error {
	if n := len(a.pending); n != 0 {
		return fmt.Errorf("missing chunks: received %d contiguous chunks with %d still buffered", a.nextSeq, n)
	}
	if totalChunks > 0 && a.nextSeq < totalChunks {
		return fmt.Errorf("missing chunks: received %d of %d", a.nextSeq, totalChunks)
	}
	return nil
}

// ChunkCount returns the number of chunks needed to carry size bytes with the
// given chunk size. Empty files or a non-positive chunk size yield zero.
func ChunkCount(size int64, chunkSize int) int {
	if size <= 0 || chunkSize <= 0 {
		return 0
	}
	return int((size + int64(chunkSize) - 1) / int64(chunkSize))
}
