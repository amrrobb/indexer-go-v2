package worker

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"indexer-go-v2/internal/config"
	"indexer-go-v2/internal/database"
	"indexer-go-v2/internal/decoder"
	redispkg "indexer-go-v2/internal/redis"
	"indexer-go-v2/internal/rpc"
	"indexer-go-v2/internal/webhook"
	"indexer-go-v2/pkg/types"
)

// ForwardWorker processes blocks in near real-time
type ForwardWorker struct {
	config        *config.Loader
	db            *database.Client
	redis         *redispkg.Client
	rpcClient     *rpc.Client
	decoder       *decoder.Decoder
	webhook       *webhook.Client
	chainID       int
	workerType    string
	logger        *logrus.Entry
	interval      time.Duration
	reloadChannel chan bool // Channel to signal config reload
}

// NewForwardWorker creates a new forward worker
func NewForwardWorker(
	config *config.Loader,
	db *database.Client,
	redis *redispkg.Client,
	rpcClient *rpc.Client,
	webhook *webhook.Client,
	chainID int,
) (*ForwardWorker, error) {
	// Parse interval from environment
	interval := 3 * time.Second
	if intervalStr := os.Getenv("FORWARD_INTERVAL"); intervalStr != "" {
		if parsed, err := time.ParseDuration(intervalStr); err == nil {
			interval = parsed
		}
	}

	logger := logrus.WithFields(logrus.Fields{
		"component":  "forward_worker",
		"chain_id":   chainID,
		"worker_type": types.WorkerTypeForward,
		"interval":    interval,
	})

	return &ForwardWorker{
		config:        config,
		db:            db,
		redis:         redis,
		rpcClient:     rpcClient,
		decoder:       decoder.NewDecoder(),
		webhook:       webhook,
		chainID:       chainID,
		workerType:    types.WorkerTypeForward,
		logger:        logger,
		interval:      interval,
		reloadChannel: make(chan bool, 1), // Buffered to avoid blocking
	}, nil
}

// Start begins the forward worker processing loop
func (w *ForwardWorker) Start(ctx context.Context) error {
	w.logger.Info("Starting forward worker")

	// Load initial configuration
	workerConfig, err := w.config.GetWorkerConfig(w.chainID)
	if err != nil {
		return fmt.Errorf("failed to load worker configuration: %w", err)
	}

	w.logger.WithFields(logrus.Fields{
		"network":            workerConfig.Network,
		"active_currencies":  len(workerConfig.ActiveCurrencies),
		"watched_addresses":  len(workerConfig.WatchedAddresses),
		"confirmations":      workerConfig.TargetConfirmations,
	}).Info("Forward worker configuration loaded")

	// Main processing loop
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Forward worker stopped")
			return ctx.Err()

		case <-w.reloadChannel:
			// Config change detected - reload configuration
			w.logger.Info("🔄 Reloading worker configuration...")
			newConfig, err := w.config.GetWorkerConfig(w.chainID)
			if err != nil {
				w.logger.WithError(err).Error("Failed to reload configuration - keeping old config")
			} else {
				workerConfig = newConfig
				w.logger.WithFields(logrus.Fields{
					"network":            workerConfig.Network,
					"active_currencies":  len(workerConfig.ActiveCurrencies),
					"watched_addresses":  len(workerConfig.WatchedAddresses),
					"confirmations":      workerConfig.TargetConfirmations,
				}).Info("✅ Configuration reloaded successfully")
			}

		case <-ticker.C:
			if err := w.processOnce(ctx, workerConfig); err != nil {
				w.logger.WithError(err).Error("Error in forward worker iteration")
			}
		}
	}
}

// TriggerConfigReload signals the worker to reload its configuration
func (w *ForwardWorker) TriggerConfigReload() {
	select {
	case w.reloadChannel <- true:
		w.logger.Debug("Config reload signal sent")
	default:
		w.logger.Debug("Config reload already pending")
	}
}

// processOnce performs a single forward worker iteration
func (w *ForwardWorker) processOnce(ctx context.Context, workerConfig *types.WorkerConfig) error {
	// Get current block number with extended timeout for ERPC
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	currentBlock, err := w.rpcClient.GetBlockNumber(ctxWithTimeout)
	if err != nil {
		w.logger.WithFields(logrus.Fields{
			"error": err.Error(),
		}).Warn("Failed to get current block number (ERPC timeout)")
		return fmt.Errorf("failed to get current block number: %w", err)
	}

	// Calculate safe range (5 confirmations back)
	safeBlock := currentBlock - 5
	if safeBlock < 0 {
		safeBlock = 0
	}

	// Get last processed block from Redis
	lastBlock, err := w.redis.GetCursor(ctx, w.chainID, w.workerType)
	if err != nil {
		return fmt.Errorf("failed to get forward cursor: %w", err)
	}

	// Calculate range: max(safeBlock-9, lastBlock+1) to safeBlock
	fromBlock := lastBlock + 1
	if fromBlock < safeBlock-9 {
		fromBlock = safeBlock - 9
	}
	toBlock := safeBlock

	// Skip if no range to process
	if fromBlock > toBlock {
		w.logger.WithFields(logrus.Fields{
			"current_block": currentBlock,
			"safe_block":     safeBlock,
			"last_block":     lastBlock,
			"from_block":     fromBlock,
			"to_block":       toBlock,
		}).Debug("No blocks to process (waiting for new blocks)")
		return nil
	}

	w.logger.WithFields(logrus.Fields{
		"current_block": currentBlock,
		"safe_block":     safeBlock,
		"last_block":     lastBlock,
		"from_block":     fromBlock,
		"to_block":       toBlock,
		"range_size":     toBlock - fromBlock + 1,
		"watched_addresses": len(workerConfig.WatchedAddresses),
		"active_currencies": len(workerConfig.ActiveCurrencies),
	}).Info("🚀 Processing forward range")

	// Process the block range
	return w.processRange(ctx, workerConfig, fromBlock, toBlock, currentBlock)
}

// processRange processes a specific block range
func (w *ForwardWorker) processRange(ctx context.Context, workerConfig *types.WorkerConfig, fromBlock, toBlock, currentBlock int64) error {
	// Build token addresses for filtering
	var tokenAddresses []string
	for _, currency := range workerConfig.ActiveCurrencies {
		if !currency.IsNative {
			tokenAddresses = append(tokenAddresses, currency.ContractAddress)
		}
	}

	// Build topics filter (Transfer events to our watched addresses)
	topics := rpc.BuildLogFilter(tokenAddresses, workerConfig.WatchedAddresses)

	w.logger.WithFields(logrus.Fields{
		"from_block": fromBlock,
		"to_block":   toBlock,
		"token_addresses": len(tokenAddresses),
		"topics": len(topics),
		"watched_addresses_count": len(workerConfig.WatchedAddresses),
	}).Info("🔍 Querying RPC logs for ERC-20 Transfers")

	// Query logs with extended timeout for ERPC
	logsCtx, logsCancel := context.WithTimeout(ctx, 90*time.Second)
	defer logsCancel()

	logs, err := w.rpcClient.GetLogs(logsCtx, fromBlock, toBlock, tokenAddresses, topics)
	if err != nil {
		w.logger.WithFields(logrus.Fields{
			"from_block": fromBlock,
			"to_block":   toBlock,
			"error":      err.Error(),
		}).Warn("Failed to get logs (ERPC timeout)")
		return fmt.Errorf("failed to get logs for range %d-%d: %w", fromBlock, toBlock, err)
	}

	w.logger.WithFields(logrus.Fields{
		"from_block":     fromBlock,
		"to_block":       toBlock,
		"logs_found":    len(logs),
		"rpc_success":   true,
	}).Info("📊 Retrieved logs from RPC")

	// Decode and process events
	currencies := convertCurrencySliceToPointers(workerConfig.ActiveCurrencies)

	w.logger.WithFields(logrus.Fields{
		"logs_count": len(logs),
		"currencies_count": len(currencies),
		"watched_addresses_count": len(workerConfig.WatchedAddresses),
	}).Info("🔓 Decoding logs for payment events")

	events, err := w.decoder.DecodeBatchLogs(logs, currencies, workerConfig.WatchedAddresses, workerConfig.ChainID)
	if err != nil {
		w.logger.WithFields(logrus.Fields{
			"error": err.Error(),
			"logs_count": len(logs),
		}).Error("Failed to decode logs")
		return fmt.Errorf("failed to decode logs: %w", err)
	}

	w.logger.WithFields(logrus.Fields{
		"from_block":     fromBlock,
		"to_block":       toBlock,
		"logs_input":     len(logs),
		"events_found":   len(events),
		"decode_success": true,
	}).Info("💳 Decoded payment events")

	// Process each event
	for _, event := range events {
		if err := w.processEvent(ctx, event, workerConfig); err != nil {
			w.logger.WithFields(logrus.Fields{
				"event_id": event.ID,
				"tx_hash":  event.TransactionHash,
				"error":    err.Error(),
			}).Error("Failed to process event")
			continue
		}
	}

	// Mark blocks as processed to avoid overlap with backfill
	for blockNum := fromBlock; blockNum <= toBlock; blockNum++ {
		// Mark in Redis for caching
		if err := w.redis.MarkBlockSeen(ctx, w.chainID, blockNum, 12*time.Hour); err != nil {
			w.logger.WithError(err).WithField("block_number", blockNum).Warn("Failed to mark block as seen")
		}

		// Mark in database for persistent tracking
		if err := w.db.MarkBlockProcessed(ctx, w.chainID, blockNum, "forward"); err != nil {
			w.logger.WithError(err).WithField("block_number", blockNum).Warn("Failed to mark block as processed in database")
		}
	}

	// Update cursor in Redis
	if err := w.redis.SetCursor(ctx, w.chainID, w.workerType, toBlock); err != nil {
		w.logger.WithError(err).Warn("Failed to update forward cursor in Redis")
	}

	// Persist cursor to database periodically (every 10 blocks)
	if toBlock%10 == 0 {
		if err := w.db.UpsertCursor(ctx, w.chainID, w.workerType, toBlock); err != nil {
			w.logger.WithError(err).Warn("Failed to persist forward cursor to database")
		}
	}

	// Log progress
	progress := float64(toBlock-fromBlock+1) / float64(currentBlock-fromBlock+1) * 100
	w.logger.WithFields(logrus.Fields{
		"from_block":     fromBlock,
		"to_block":       toBlock,
		"last_block":     currentBlock,
		"progress":       fmt.Sprintf("%.2f%%", progress),
	}).Info("Forward range completed")

	return nil
}

// processEvent handles a single payment event
func (w *ForwardWorker) processEvent(ctx context.Context, event *types.PaymentEvent, workerConfig *types.WorkerConfig) error {
	// Check if event already processed (deduplication)
	if processed, err := w.redis.IsEventProcessed(ctx, event.ID); err != nil {
		return fmt.Errorf("failed to check if event is processed: %w", err)
	} else if processed {
		w.logger.WithFields(logrus.Fields{
			"event_id": event.ID,
			"tx_hash": event.TransactionHash,
		}).Debug("Event already processed")
		return nil
	}

	// Add event to deduplication set
	if err := w.redis.AddEvent(ctx, event.ID); err != nil {
		return fmt.Errorf("failed to add event to deduplication set: %w", err)
	}

	// Send "detected" webhook (5 confirmations)
	if err := w.webhook.SendPaymentWebhook(ctx, event, types.StatusDetected, 5); err != nil {
		return fmt.Errorf("failed to send detected webhook: %w", err)
	}

	// Schedule confirmation check when event reaches target confirmations
	if currentBlock, err := w.rpcClient.GetBlockNumber(ctx); err == nil {
		targetBlock := event.BlockNumber + int64(workerConfig.TargetConfirmations)
		targetTime := time.Now().Add(time.Duration(workerConfig.TargetConfirmations) * 15 * time.Second) // Approximate 15s per block

		w.logger.WithFields(logrus.Fields{
			"event_id":     event.ID,
			"current_block": currentBlock,
			"target_block":  targetBlock,
			"confirmations": workerConfig.TargetConfirmations,
		}).Debug("Scheduling confirmation check")

		// Store event details for confirmation processing
		eventDetails := &redispkg.EventDetails{
			EventID:         event.ID,
			ChainID:         event.ChainID,
			TransactionHash: event.TransactionHash,
			BlockNumber:     event.BlockNumber,
			FromAddress:     event.FromAddress,
			ToAddress:       event.ToAddress,
			TokenAddress:    event.TokenAddress,
			TokenSymbol:     event.TokenSymbol,
			TokenName:       event.TokenName,
			Amount:          event.Amount,
			AmountFormatted: event.AmountFormatted,
			FirstDetectedAt: time.Now(),
		}

		if err := w.redis.SetEventDetails(ctx, event.ID, eventDetails, 24*time.Hour); err != nil {
			w.logger.WithError(err).Warn("Failed to store event details")
		}

		if err := w.redis.ScheduleConfirmation(ctx, event.ID, targetBlock, targetTime); err != nil {
			w.logger.WithError(err).Warn("Failed to schedule confirmation check")
		}
	}

	// Update event with initial status
	event.Status = types.StatusDetected
	event.Confirmations = 5
	event.FirstDetectedAt = &[]time.Time{time.Now()}[0]

	return nil
}

// Health checks the health of the forward worker
func (w *ForwardWorker) Health(ctx context.Context) error {
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
		return fmt.Errorf("database health check failed: w", err)
	}

	// Check RPC connection
	_, err = w.rpcClient.GetBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("RPC health check failed: w", err)
	}

	// Check webhook configuration
	if !w.webhook.IsEnabled() {
		return fmt.Errorf("webhook not configured")
	}

	return nil
}

// GetStatus returns current worker status information
func (w *ForwardWorker) GetStatus(ctx context.Context) (*types.WorkerStatus, error) {
	// Get current block number
	currentBlock, err := w.rpcClient.GetBlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current block: %w", err)
	}

	// Get last processed block
	lastBlock, err := w.redis.GetCursor(ctx, w.chainID, w.workerType)
	if err != nil {
		return nil, fmt.Errorf("failed to get cursor: %w", err)
	}

	// Calculate blocks processed in last iteration
	blocksProcessed := lastBlock
	if currentBlock > lastBlock {
		blocksProcessed = currentBlock - lastBlock
	}

	status := &types.WorkerStatus{
		ChainID:        w.chainID,
		WorkerType:     w.workerType,
		CurrentBlock:   currentBlock,
		LastProcessed:  lastBlock,
		BlocksPerSec:   float64(blocksProcessed) / w.interval.Seconds(),
		EventsPerSec:   0, // Would need to track this separately
		LastActivity:   time.Now(),
		IsHealthy:      true,
		ErrorCount:     0,
		LastError:      "",
	}

	return status, nil
}

// Stop performs cleanup when stopping the worker
func (w *ForwardWorker) Stop() error {
	w.logger.Info("Stopping forward worker")
	// Cleanup would go here if needed
	return nil
}

// convertCurrencySliceToPointers converts []types.Currency to []*types.Currency
func convertCurrencySliceToPointers(currencies []types.Currency) []*types.Currency {
	pointers := make([]*types.Currency, len(currencies))
	for i := range currencies {
		pointers[i] = &currencies[i]
	}
	return pointers
}