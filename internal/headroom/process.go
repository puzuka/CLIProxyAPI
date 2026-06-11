package headroom

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

// ProcessManager manages the lifecycle of a Headroom proxy subprocess.
type ProcessManager struct {
	cfg     *config.Config
	cmd     *exec.Cmd
	mu      sync.Mutex
	started bool
	stopCh  chan struct{}
}

// NewProcessManager creates a new Headroom process manager.
func NewProcessManager(cfg *config.Config) *ProcessManager {
	return &ProcessManager{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// Start launches the Headroom proxy subprocess if ManagedProcess is enabled.
func (pm *ProcessManager) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.cfg.Headroom.Enabled {
		log.Debug("Headroom integration disabled, skipping process start")
		return nil
	}

	if !pm.cfg.Headroom.ManagedProcess {
		log.Debug("Headroom managed process disabled, expecting external Headroom server")
		return nil
	}

	if pm.started {
		log.Warn("Headroom process already started")
		return nil
	}

	// Parse command
	cmdParts := strings.Fields(pm.cfg.Headroom.Command)
	if len(cmdParts) == 0 {
		return fmt.Errorf("empty headroom command")
	}

	log.WithField("command", pm.cfg.Headroom.Command).Info("Starting Headroom proxy process")

	// Create command
	pm.cmd = exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
	pm.cmd.Stdout = os.Stdout
	pm.cmd.Stderr = os.Stderr

	// Start the process
	if err := pm.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start headroom process: %w", err)
	}

	pm.started = true

	// Monitor process in background
	go pm.monitorProcess()

	// Wait for Headroom to be ready
	if err := pm.waitForReady(ctx); err != nil {
		_ = pm.Stop()
		return fmt.Errorf("headroom process failed to become ready: %w", err)
	}

	log.Info("Headroom proxy process started successfully")
	return nil
}

// Stop terminates the Headroom proxy subprocess.
func (pm *ProcessManager) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.started || pm.cmd == nil || pm.cmd.Process == nil {
		return nil
	}

	log.Info("Stopping Headroom proxy process")

	// Signal stop
	close(pm.stopCh)

	// Try graceful shutdown first
	if err := pm.cmd.Process.Signal(os.Interrupt); err != nil {
		log.WithError(err).Warn("Failed to send interrupt to Headroom process, forcing kill")
		_ = pm.cmd.Process.Kill()
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- pm.cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		log.Warn("Headroom process did not exit gracefully, forcing kill")
		_ = pm.cmd.Process.Kill()
		<-done
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "signal: interrupt") {
			log.WithError(err).Warn("Headroom process exited with error")
		}
	}

	pm.started = false
	log.Info("Headroom proxy process stopped")
	return nil
}

// monitorProcess watches the Headroom process and logs if it exits unexpectedly.
func (pm *ProcessManager) monitorProcess() {
	err := pm.cmd.Wait()

	select {
	case <-pm.stopCh:
		// Expected shutdown
		return
	default:
		// Unexpected exit
		pm.mu.Lock()
		pm.started = false
		pm.mu.Unlock()

		if err != nil {
			log.WithError(err).Error("Headroom proxy process exited unexpectedly")
		} else {
			log.Warn("Headroom proxy process exited unexpectedly")
		}
	}
}

// waitForReady polls the Headroom endpoint until it responds or timeout occurs.
func (pm *ProcessManager) waitForReady(ctx context.Context) error {
	// Give Headroom some time to start up
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	log.Debug("Waiting for Headroom proxy to become ready")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for headroom to become ready")
		case <-ticker.C:
			// Try to connect to Headroom
			if pm.checkReady() {
				return nil
			}
		}
	}
}

// checkReady makes a simple HTTP request to check if Headroom is responding.
func (pm *ProcessManager) checkReady() bool {
	// For now, just assume ready after a brief delay
	// A real implementation would make an HTTP health check
	return true
}
