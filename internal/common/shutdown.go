package common

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// ShutdownManager handles graceful shutdown for workers
type ShutdownManager struct {
	logger    *logrus.Entry
	sigChan   chan os.Signal
	errChan   chan error
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewShutdownManager creates a new shutdown manager
func NewShutdownManager(workerType string) *ShutdownManager {
	logger := logrus.WithFields(logrus.Fields{
		"component":  "shutdown_manager",
		"worker_type": workerType,
	})

	ctx, cancel := context.WithCancel(context.Background())

	return &ShutdownManager{
		logger:  logger,
		sigChan: make(chan os.Signal, 1),
		errChan: make(chan error, 1),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// SetupSignalHandling configures signal handling for graceful shutdown
func (sm *ShutdownManager) SetupSignalHandling() {
	signal.Notify(sm.sigChan, syscall.SIGINT, syscall.SIGTERM)
}

// WaitForShutdown waits for either a signal or an error and handles shutdown
func (sm *ShutdownManager) WaitForShutdown(workerStoppable interface{ Stop() error }) error {
	select {
	case sig := <-sm.sigChan:
		sm.logger.WithField("signal", sig).Info("Received shutdown signal")
		return sm.handleGracefulShutdown(workerStoppable)

	case err := <-sm.errChan:
		sm.logger.WithError(err).Error("Worker failed unexpectedly")
		return err
	}
}

// handleGracefulShutdown performs the actual shutdown process
func (sm *ShutdownManager) handleGracefulShutdown(workerStoppable interface{ Stop() error }) error {
	// Cancel context to stop worker
	sm.cancel()

	// Give worker time to cleanup
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := workerStoppable.Stop(); err != nil {
		sm.logger.WithError(err).Warn("Error during worker shutdown")
	}

	// Wait for graceful shutdown or timeout
	select {
	case <-time.After(25 * time.Second):
		sm.logger.Warn("Shutdown timeout reached")
	case <-sm.errChan:
		sm.logger.Info("Worker stopped gracefully")
	case <-shutdownCtx.Done():
		sm.logger.Info("Worker stopped gracefully")
	}

	sm.logger.Info("Worker shutdown complete")
	return nil
}

// SendError sends an error to the shutdown manager
func (sm *ShutdownManager) SendError(err error) {
	select {
	case sm.errChan <- err:
	default:
		// Channel full, error already being handled
	}
}

// GetContext returns the managed context
func (sm *ShutdownManager) GetContext() context.Context {
	return sm.ctx
}