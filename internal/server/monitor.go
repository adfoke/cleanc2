package server

import (
	"go.uber.org/zap"

	"coc2/internal/protocol"
)

func (s *Service) handleMetricsReport(report protocol.MetricsReport) {
	if err := s.store.SaveAgentMetrics(report); err != nil {
		s.logger.Warn("save metrics", zap.String("agent_id", report.AgentID), zap.Error(err))
		return
	}
	s.plugins.Trigger("metrics_report", report)
}

func (s *Service) activeTransfersCount() int {
	s.transferMu.RLock()
	defer s.transferMu.RUnlock()
	return len(s.transfers)
}
