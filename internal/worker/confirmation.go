package worker

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"indexer-go-v2/internal/config"
	"indexer-go-v2/internal/database"
	redispkg "indexer-go-v2/internal/redis"
	"indexer-go-v2/internal/rpc"
	"indexer-go-v2/internal/webhook"
	"indexer-go-v2/pkg/types"
)

// ConfirmationWorker handles confirmation checking for detected payments
type ConfirmationWorker struct {
	config      *config.Loader
	db          *database.Client
	redis       *redispkg.Client
	rpcClient   *rpc.Client
	webhook     *webhook.Client
	chainID     int
	workerType  string
	logger      *logrus.Entry
	interval    time.Duration
}

// NewConfirmationWorker creates a new confirmation worker
func NewConfirmationWorker(
	config *config.Loader,
	db *database.Client,
	redis *redispkg.Client,
	rpcClient *rpc.Client,
	webhook *webhook.Client,
	chainID int,
) (*ConfirmationWorker, error) {
	// Parse interval from environment
	interval := 30 * time.Second
	if intervalStr := os.Getenv("CONFIRMATION_INTERVAL"); intervalStr != "" {
		if parsed, err := time.ParseDuration(intervalStr); err == nil {
			interval = parsed
		}
	}

	logger := logrus.WithFields(logrus.Fields{
		"component":  "confirmation_worker",
		"chain_id":   chainID,
		"worker_type": types.WorkerTypeConfirmation,
		"interval":   interval,
	})

	return &ConfirmationWorker{
		config:     config,
		db:         db,
		redis:      redis,
		rpcClient:  rpcClient,
		webhook:    webhook,
		chainID:    chainID,
		workerType: types.WorkerTypeConfirmation,
		logger:     logger,
		interval:   interval,
	}, nil
}

// Start begins the confirmation worker processing loop
func (w *ConfirmationWorker) Start(ctx context.Context) error {
	w.logger.Info("Starting confirmation worker")

	// Load configuration
	workerConfig, err := w.config.GetWorkerConfig(w.chainID)
	if err != nil {
		return fmt.Errorf("failed to load worker configuration: %w", err)
	}

	w.logger.WithFields(logrus.Fields{
		"network":            workerConfig.Network,
		"target_confirmations": workerConfig.TargetConfirmations,
	}).Info("Confirmation worker configuration loaded")

	// Main processing loop
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Confirmation worker stopped")
			return ctx.Err()

		case <-time.After(w.interval):
			if err := w.processOnce(ctx, workerConfig); err != nil {
				w.logger.WithError(err).Error("Error in confirmation worker iteration")
			}
		}
	}
}

// processOnce performs a single confirmation worker iteration
func (w *ConfirmationWorker) processOnce(ctx context.Context, workerConfig *types.WorkerConfig) error {
	// Get scheduled confirmations from Redis
	scheduledEvents, err := w.redis.GetScheduledConfirmations(ctx, w.chainID)
	if err != nil {
		return fmt.Errorf("failed to get scheduled confirmations: %w", err)
	}

	if len(scheduledEvents) == 0 {
		w.logger.Debug("No scheduled confirmations to process")
		return nil
	}

	w.logger.WithField("scheduled_count", len(scheduledEvents)).Debug("Processing scheduled confirmations")

	// Get current block number once
	currentBlock, err := w.rpcClient.GetBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current block number: %w", err)
	}

	// Process each scheduled event
	for _, eventID := range scheduledEvents {
		if err := w.processScheduledConfirmation(ctx, eventID, workerConfig, currentBlock); err != nil {
			w.logger.WithFields(logrus.Fields{
				"event_id": eventID,
				"error":    err.Error(),
			}).Error("Failed to process scheduled confirmation")
			continue
		}
	}

	return nil
}

// processScheduledConfirmation handles a single scheduled confirmation
func (w *ConfirmationWorker) processScheduledConfirmation(ctx context.Context, eventID string, workerConfig *types.WorkerConfig, currentBlock int64) error {
	// Check if confirmation is ready
	ready, err := w.redis.IsConfirmationReady(ctx, eventID, currentBlock)
	if err != nil {
		return fmt.Errorf("failed to check confirmation readiness: %w", err)
	}

	if !ready {
		// Not ready yet, skip for now
		return nil
	}

	// Get event details from Redis
	eventDetails, err := w.redis.GetEventDetails(ctx, eventID)
	if err != nil {
		return fmt.Errorf("failed to get event details: %w", err)
	}

	if eventDetails == nil {
		// Event not found, remove from schedule
		w.logger.WithField("event_id", eventID).Warn("Event details not found, removing from schedule")
		w.redis.RemoveConfirmation(ctx, eventID)
		return nil
	}

	// Calculate current confirmations
	confirmations := currentBlock - eventDetails.BlockNumber
	if confirmations < 0 {
		confirmations = 0
	}

	// Check if we've reached target confirmations
	targetConfirmations := int64(workerConfig.TargetConfirmations)
	if confirmations < targetConfirmations {
		// Not enough confirmations yet, keep in schedule
		w.logger.WithFields(logrus.Fields{
			"event_id":      eventID,
			"confirmations": confirmations,
			"target":        targetConfirmations,
		}).Debug("Insufficient confirmations, keeping scheduled")
		return nil
	}

	// Get transaction receipt to verify finality
	receipt, err := w.rpcClient.GetTransactionReceipt(ctx, eventDetails.TransactionHash)
	if err != nil {
		return fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	if receipt == nil {
		// Transaction not found or failed, remove from schedule
		w.logger.WithFields(logrus.Fields{
			"event_id": eventID,
			"tx_hash":  eventDetails.TransactionHash,
		}).Warn("Transaction receipt not found, removing from schedule")
		w.redis.RemoveConfirmation(ctx, eventID)
		return nil
	}

	// Verify transaction was successful
	if receipt.Status == "0x0" {
		// Transaction failed, remove from schedule
		w.logger.WithFields(logrus.Fields{
			"event_id": eventID,
			"tx_hash":  eventDetails.TransactionHash,
		}).Info("Transaction failed, removing from schedule")
		w.redis.RemoveConfirmation(ctx, eventID)
		return nil
	}

	// Create payment event for webhook
	event := &types.PaymentEvent{
		ID:              eventID,
		ChainID:         w.chainID,
		TransactionHash: eventDetails.TransactionHash,
		BlockNumber:     eventDetails.BlockNumber,
		FromAddress:     eventDetails.FromAddress,
		ToAddress:       eventDetails.ToAddress,
		TokenAddress:    eventDetails.TokenAddress,
		TokenSymbol:     eventDetails.TokenSymbol,
		TokenName:       eventDetails.TokenName,
		Amount:          eventDetails.Amount,
		AmountFormatted: eventDetails.AmountFormatted,
		Status:          types.StatusDetected, // Will be updated by webhook client
		Confirmations:   int(confirmations),
		FirstDetectedAt: &eventDetails.FirstDetectedAt,
		Timestamp:       time.Now(),
	}

	// Send confirmation webhook
	if err := w.webhook.SendConfirmationWebhook(ctx, event, int(confirmations)); err != nil {
		return fmt.Errorf("failed to send confirmation webhook: %w", err)
	}

	// Mark as confirmed in Redis
	if err := w.redis.MarkEventConfirmed(ctx, eventID, int(confirmations)); err != nil {
		w.logger.WithError(err).Warn("Failed to mark event as confirmed in Redis")
	}

	// Remove from confirmation schedule
	if err := w.redis.RemoveConfirmation(ctx, eventID); err != nil {
		w.logger.WithError(err).Warn("Failed to remove event from confirmation schedule")
	}

	w.logger.WithFields(logrus.Fields{
		"event_id":      eventID,
		"tx_hash":       eventDetails.TransactionHash,
		"confirmations": confirmations,
		"token_symbol":  eventDetails.TokenSymbol,
		"amount":        eventDetails.AmountFormatted,
	}).Info("Payment confirmed and webhook sent")

	return nil
}

// Health checks the health of the confirmation worker
func (w *ConfirmationWorker) Health(ctx context.Context) error {
	// Check configuration
	_, err := w.config.GetWorkerConfig(w.chainID)
	if err != nil {
		return fmt.Errorf("worker configuration error: %w", err)
	}

	// Check Redis connection
	if err := w.redis.Health(); err != nil {
		return fmt.Errorf("Redis health check failed: %w", err)
	}

	// Check database connection
	if err := w.db.Health(); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}

	// Check RPC connection
	_, err = w.rpcClient.GetBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("RPC health check failed: %w", err)
	}

	// Check webhook configuration
	if !w.webhook.IsEnabled() {
		return fmt.Errorf("webhook not configured")
	}

	return nil
}

// GetStatus returns current worker status information
func (w *ConfirmationWorker) GetStatus(ctx context.Context) (*types.WorkerStatus, error) {
	// Get current block number
	currentBlock, err := w.rpcClient.GetBlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current block: %w", err)
	}

	// Get scheduled confirmations count
	scheduledEvents, err := w.redis.GetScheduledConfirmations(ctx, w.chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get scheduled confirmations: %w", err)
	}

	status := &types.WorkerStatus{
		ChainID:        w.chainID,
		WorkerType:     w.workerType,
		CurrentBlock:   currentBlock,
		LastProcessed:  0, // Not applicable for confirmation worker
		BlocksPerSec:   0,
		EventsPerSec:   float64(len(scheduledEvents)) / w.interval.Seconds(),
		LastActivity:   time.Now(),
		IsHealthy:      true,
		ErrorCount:     0,
		LastError:      "",
	}

	return status, nil
}

// Stop performs cleanup when stopping the worker
func (w *ConfirmationWorker) Stop() error {
	w.logger.Info("Stopping confirmation worker")
	// Cleanup would go here if needed
	return nil
}