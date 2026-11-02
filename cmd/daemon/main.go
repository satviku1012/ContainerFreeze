package main

import (
	"os"
	"path/filepath"

	"github.com/podfreeze/core/internal/daemon"
	"github.com/podfreeze/core/pkg/logger"
)

func main() {
	log := logger.New()
	log.Info("starting podfreeze daemon")
	checkpointDir := os.Getenv("PODFREEZE_CHECKPOINT_DIR")
	if checkpointDir == "" {
		checkpointDir = filepath.Join(os.TempDir(), "podfreeze-checkpoints")
	}
	criuPath := os.Getenv("PODFREEZE_CRIU_PATH")
	if err := daemon.ListenAndServe(envOr("PODFREEZE_GRPC_ADDR", ":50051"), criuPath, checkpointDir, log); err != nil {
		log.Error("daemon failed", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
