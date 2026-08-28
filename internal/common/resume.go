package common

import (
	"errors"
	"io"
	"os"
)

// ResumeOffset returns the largest chunk-aligned byte offset covered by the
// file at path, or 0 if the file does not exist. It lets a receiver report how
// much of a transfer it has already persisted so the sender can skip ahead.
func ResumeOffset(path string, chunkSize int) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if info.IsDir() {
		return 0, errors.New("resume path is a directory: " + path)
	}
	if chunkSize <= 0 {
		return 0, nil
	}
	n := info.Size()
	return n / int64(chunkSize) * int64(chunkSize), nil
}

// OpenPartialFile opens (or creates) path for appending resume data. A zero
// offset truncates the file for a fresh transfer; a non-zero offset keeps the
// existing prefix and seeks to offset.
func OpenPartialFile(path string, offset int64) (*os.File, error) {
	if offset <= 0 {
		return os.Create(path)
	}
	if err := os.Truncate(path, offset); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
