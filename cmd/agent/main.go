package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"cleanc2/internal/agent"
)

func main() {
	cfg := agent.Config{}
	var tags string
	flag.StringVar(&cfg.ServerURL, "server", "ws://127.0.0.1:8080/ws/agent", "server websocket url")
	flag.StringVar(&cfg.Token, "token", "cleanc2-dev-token", "shared agent token")
	flag.StringVar(&cfg.AgentID, "agent-id", "", "agent id")
	flag.StringVar(&tags, "tags", "", "comma separated tags")
	flag.DurationVar(&cfg.HeartbeatInterval, "heartbeat", 30*time.Second, "heartbeat interval")
	flag.DurationVar(&cfg.MaxBackoff, "max-backoff", 30*time.Second, "max reconnect backoff")
	flag.StringVar(&cfg.ServerName, "server-name", "", "tls server name")
	flag.StringVar(&cfg.CACertFile, "ca-cert", "", "ca cert file")
	flag.StringVar(&cfg.ClientCertFile, "client-cert", "", "client cert file")
	flag.StringVar(&cfg.ClientKeyFile, "client-key", "", "client key file")
	flag.Parse()

	if tags != "" {
		cfg.Tags = strings.Split(tags, ",")
	}

	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	client, err := agent.New(cfg, logger)
	if err != nil {
		logger.Fatal("create agent", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Fatal("run agent", zap.Error(err))
	}
}
