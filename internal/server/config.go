package server

import "time"

type Config struct {
	ListenAddr string `yaml:"listen"`
	AuthToken  string `yaml:"token"`
	APIToken   string `yaml:"api_token"`
	DBPath     string `yaml:"db"`
	PluginDir  string `yaml:"plugins"`

	TLSCertFile  string `yaml:"tls_cert"`
	TLSKeyFile   string `yaml:"tls_key"`
	ClientCAFile string `yaml:"client_ca"`

	WriteWait  time.Duration `yaml:"write_wait"`
	PongWait   time.Duration `yaml:"pong_wait"`
	PingPeriod time.Duration `yaml:"ping_period"`
}
