package main

import (
	"os"

	"github.com/podfreeze/core/internal/proxy"
	"github.com/podfreeze/core/pkg/logger"
)

func main() {
	log := logger.New()
	client, err := proxy.NewClient(envOr("PODFREEZE_GRPC_ADDR", "127.0.0.1:50051"), log)
	if err != nil {
		log.Error("failed to initialize grpc client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	server := proxy.NewServer(envOr("PODFREEZE_PROXY_ADDR", ":8081"), log, client)
	log.Info("starting podfreeze proxy")
	if err := server.ListenAndServe(); err != nil {
		log.Error("proxy failed", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
