package agent

import "time"

type Config struct {
	ServerURL string
	Token     string
	AgentID   string
	Tags      []string

	HeartbeatInterval time.Duration
	MaxBackoff        time.Duration

	ServerName     string
	CACertFile     string
	ClientCertFile string
	ClientKeyFile  string
}
