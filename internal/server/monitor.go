package server

import (
	"go.uber.org/zap"

	"cleanc2/internal/protocol"
)

func (s *Service) handleMetricsReport(report protocol.MetricsReport) {
	metrics := AgentMetrics{
		AgentID:            report.AgentID,
		Timestamp:          report.Timestamp,
		UptimeSecs:         report.UptimeSecs,
		CPUCount:           report.CPUCount,
		Goroutines:         report.Goroutines,
		ProcessMemoryBytes: report.ProcessMemoryBytes,
		RootDiskTotalBytes: report.RootDiskTotalBytes,
		RootDiskFreeBytes:  report.RootDiskFreeBytes,
	}
	if err := s.store.SaveAgentMetrics(metrics); err != nil {
		s.logger.Warn("save metrics", zap.String("agent_id", report.AgentID), zap.Error(err))
		return
	}
	s.plugins.Trigger("metrics_report", metrics)
}

func (s *Service) activeTransfersCount() int {
	s.transferMu.RLock()
	defer s.transferMu.RUnlock()
	return len(s.transfers)
}
