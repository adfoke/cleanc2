package server

import "time"

type Config struct {
	ListenAddr string `yaml:"listen"`
	AuthToken  string `yaml:"token"`
	APIToken   string `yaml:"api_token"`
	DBPath     string `yaml:"db"`
	PluginDir  string `yaml:"plugins"`

	// OperatorUDSPath is the Unix socket serving the operator (CLI/API)
	// plane. It defaults to ./cleanc2.sock; filesystem permissions are the
	// access boundary, so no token is required on this plane.
	OperatorUDSPath string `yaml:"operator_uds"`
	// OperatorListen additionally exposes the operator plane on TCP.
	// When set, token auth is enforced for every request on it.
	OperatorListen string `yaml:"operator_listen"`

	TLSCertFile  string `yaml:"tls_cert"`
	TLSKeyFile   string `yaml:"tls_key"`
	ClientCAFile string `yaml:"client_ca"`

	RequireTLS bool `yaml:"require_tls"`

	WriteWait  time.Duration `yaml:"write_wait"`
	PongWait   time.Duration `yaml:"pong_wait"`
	PingPeriod time.Duration `yaml:"ping_period"`
}
