package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var validContainerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Manager runs CRIU against per-container checkpoint directories.
type Manager struct {
	Logger        *slog.Logger
	CRIUPath      string
	CheckpointDir string

	mu       sync.Mutex
	active   map[string]*restoreCall
	restored map[string]bool
}

type restoreCall struct {
	done chan struct{}
	err  error
}

func NewManager(logger *slog.Logger, criuPath, checkpointDir string) *Manager {
	return &Manager{Logger: logger, CRIUPath: criuPath, CheckpointDir: checkpointDir,
		active: make(map[string]*restoreCall), restored: make(map[string]bool)}
}

// CheckpointContainer saves a process tree and lets CRIU terminate it after a
// successful dump. The image directory is unique to the validated container ID.
func (m *Manager) CheckpointContainer(ctx context.Context, containerID string, pid int) error {
	dir, err := m.containerDir(containerID)
	if err != nil {
		return err
	}
	if pid <= 0 {
		return fmt.Errorf("pid must be positive")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create checkpoint directory: %w", err)
	}
	if err := m.run(ctx, "dump", "--tree", strconv.Itoa(pid), "--images-dir", dir,
		"--work-dir", dir, "--tcp-established", "--shell-job", "--manage-cgroups=soft"); err != nil {
		return fmt.Errorf("criu dump: %w", err)
	}
	m.mu.Lock()
	delete(m.restored, containerID)
	m.mu.Unlock()
	if m.Logger != nil {
		m.Logger.Info("container checkpointed", "container_id", containerID, "pid", pid, "images_dir", dir)
	}
	return nil
}

// RestoreContainer restores a checkpoint once. Concurrent wake requests for
// the same container share the in-flight CRIU command.
func (m *Manager) RestoreContainer(ctx context.Context, containerID string) error {
	dir, err := m.containerDir(containerID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.restored[containerID] {
		m.mu.Unlock()
		return nil
	}
	if call, ok := m.active[containerID]; ok {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-call.done:
			return call.err
		}
	}
	call := &restoreCall{done: make(chan struct{})}
	m.active[containerID] = call
	m.mu.Unlock()

	if m.Logger != nil {
		m.Logger.Info("restoring container", "container_id", containerID, "images_dir", dir)
	}
	call.err = m.run(ctx, "restore", "--images-dir", dir, "--work-dir", dir,
		"--restore-detached", "--tcp-established", "--shell-job", "--manage-cgroups=soft")

	m.mu.Lock()
	if call.err == nil {
		m.restored[containerID] = true
	}
	delete(m.active, containerID)
	close(call.done)
	m.mu.Unlock()
	if call.err != nil {
		return fmt.Errorf("criu restore: %w", call.err)
	}
	return nil
}

func (m *Manager) containerDir(containerID string) (string, error) {
	if !validContainerID.MatchString(containerID) || containerID == "." || containerID == ".." {
		return "", fmt.Errorf("invalid container id %q", containerID)
	}
	if m.CheckpointDir == "" {
		return "", errors.New("checkpoint directory is not configured")
	}
	return filepath.Join(m.CheckpointDir, containerID), nil
}

func (m *Manager) run(ctx context.Context, args ...string) error {
	path := m.CRIUPath
	if path == "" {
		path = "criu"
	}
	cmd := exec.CommandContext(ctx, path, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}
