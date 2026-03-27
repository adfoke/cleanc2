package agent

import (
	"runtime"
	"syscall"
	"time"

	"cleanc2/internal/protocol"
)

func (c *Client) collectMetrics() (protocol.MetricsReport, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	var total uint64
	var free uint64
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		total = stat.Blocks * uint64(stat.Bsize)
		free = stat.Bavail * uint64(stat.Bsize)
	}

	return protocol.MetricsReport{
		AgentID:            c.agentID,
		Timestamp:          time.Now().UTC(),
		UptimeSecs:         int64(time.Since(c.startedAt).Seconds()),
		CPUCount:           runtime.NumCPU(),
		Goroutines:         runtime.NumGoroutine(),
		ProcessMemoryBytes: mem.Sys,
		RootDiskTotalBytes: total,
		RootDiskFreeBytes:  free,
	}, nil
}
