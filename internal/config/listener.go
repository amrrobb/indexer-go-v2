package config

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

// ConfigChangeNotification represents a notification from the database
type ConfigChangeNotification struct {
	Table     string    `json:"table"`
	Operation string    `json:"operation"`
	NetworkID string    `json:"network_id"`
	Timestamp time.Time `json:"timestamp"`
}

// ConfigChangeListener listens for database changes and triggers config reload
type ConfigChangeListener struct {
	pool          *pgxpool.Pool
	loader        *Loader
	logger        *logrus.Entry
	reloadChannel chan ConfigChangeNotification
}

// NewConfigChangeListener creates a new config change listener
func NewConfigChangeListener(pool *pgxpool.Pool, loader *Loader) *ConfigChangeListener {
	return &ConfigChangeListener{
		pool:          pool,
		loader:        loader,
		logger:        logrus.WithField("component", "config_listener"),
		reloadChannel: make(chan ConfigChangeNotification, 100),
	}
}

// Start begins listening for PostgreSQL NOTIFY events
func (l *ConfigChangeListener) Start(ctx context.Context) error {
	l.logger.Info("Starting config change listener")

	// Acquire a dedicated connection for LISTEN
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for LISTEN: %w", err)
	}
	defer conn.Release()

	// Start listening on the 'config_change' channel
	_, err = conn.Exec(ctx, "LISTEN config_change")
	if err != nil {
		return fmt.Errorf("failed to LISTEN on config_change channel: %w", err)
	}

	l.logger.Info("Subscribed to config_change notifications")

	// Start goroutine to process reload requests
	go l.processReloadQueue(ctx)

	// Listen for notifications
	for {
		select {
		case <-ctx.Done():
			l.logger.Info("Config change listener stopped")
			return ctx.Err()
		default:
			// Wait for notification with timeout
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				l.logger.WithError(err).Error("Error waiting for notification")
				time.Sleep(1 * time.Second)
				continue
			}

			// Parse notification payload
			var change ConfigChangeNotification
			if err := json.Unmarshal([]byte(notification.Payload), &change); err != nil {
				l.logger.WithError(err).WithField("payload", notification.Payload).
					Error("Failed to parse notification payload")
				continue
			}

			l.logger.WithFields(logrus.Fields{
				"table":      change.Table,
				"operation":  change.Operation,
				"network_id": change.NetworkID,
			}).Info("Received config change notification")

			// Queue reload (non-blocking)
			select {
			case l.reloadChannel <- change:
			default:
				l.logger.Warn("Reload channel full, skipping duplicate reload")
			}
		}
	}
}

// processReloadQueue processes config reload requests with debouncing
func (l *ConfigChangeListener) processReloadQueue(ctx context.Context) {
	// Debounce timer - wait for changes to settle before reloading
	debounceTimer := time.NewTimer(0)
	debounceTimer.Stop()

	var pendingChanges []ConfigChangeNotification
	debounceDuration := 2 * time.Second // Wait 2 seconds after last change

	for {
		select {
		case <-ctx.Done():
			return

		case change := <-l.reloadChannel:
			// Add to pending changes
			pendingChanges = append(pendingChanges, change)

			// Reset debounce timer
			debounceTimer.Reset(debounceDuration)

			l.logger.WithField("pending_count", len(pendingChanges)).
				Debug("Config change queued, waiting for more changes")

		case <-debounceTimer.C:
			// Debounce period elapsed - perform reload
			if len(pendingChanges) == 0 {
				continue
			}

			l.logger.WithField("change_count", len(pendingChanges)).
				Info("Debounce period elapsed, reloading configuration")

			// Group changes by network_id to reload only affected chains
			affectedNetworks := make(map[string]bool)
			for _, change := range pendingChanges {
				affectedNetworks[change.NetworkID] = true
			}

			// Reload configuration
			if err := l.loader.RefreshConfiguration(ctx); err != nil {
				l.logger.WithError(err).Error("Failed to refresh configuration")
			} else {
				l.logger.WithField("affected_networks", len(affectedNetworks)).
					Info("Configuration reloaded successfully")
			}

			// Clear pending changes
			pendingChanges = nil
		}
	}
}

// GetReloadChannel returns the channel that emits reload events
// Workers can use this to know when to re-fetch their config
func (l *ConfigChangeListener) GetReloadChannel() <-chan ConfigChangeNotification {
	return l.reloadChannel
}
