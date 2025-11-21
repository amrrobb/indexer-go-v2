package worker

import (
	"context"
	"fmt"
	"os"
	"strconv"
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

// BackfillWorker processes historical blocks to fill gaps
type BackfillWorker struct {
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
	windowSize    int64
	reloadChannel chan bool // Channel to signal config reload
}

// NewBackfillWorker creates a new backfill worker
func NewBackfillWorker(
	config *config.Loader,
	db *database.Client,
	redis *redispkg.Client,
	rpcClient *rpc.Client,
	webhook *webhook.Client,
	chainID int,
) (*BackfillWorker, error) {
	// Parse interval from environment
	interval := 30 * time.Second
	if intervalStr := os.Getenv("BACKFILL_INTERVAL"); intervalStr != "" {
		if parsed, err := time.ParseDuration(intervalStr); err == nil {
			interval = parsed
		}
	}

	// Parse window size
	windowSize := int64(5000)
	if windowStr := os.Getenv("BACKFILL_WINDOW_SIZE"); windowStr != "" {
		if parsed, err := strconv.ParseInt(windowStr, 10, 64); err == nil {
			windowSize = parsed
		}
	}

	logger := logrus.WithFields(logrus.Fields{
		"component":   "backfill_worker",
		"chain_id":    chainID,
		"worker_type": types.WorkerTypeBackfill,
		"interval":    interval,
		"window_size":  windowSize,
	})

	return &BackfillWorker{
		config:        config,
		db:            db,
		redis:         redis,
		rpcClient:     rpcClient,
		decoder:       decoder.NewDecoder(),
		webhook:       webhook,
		chainID:       chainID,
		workerType:    types.WorkerTypeBackfill,
		logger:        logger,
		interval:      interval,
		windowSize:    windowSize,
		reloadChannel: make(chan bool, 1), // Buffered to avoid blocking
	}, nil
}

// Start begins the backfill worker processing loop
func (w *BackfillWorker) Start(ctx context.Context) error {
	w.logger.Info("Starting backfill worker")

	// Load initial configuration
	workerConfig, err := w.config.GetWorkerConfig(w.chainID)
	if err != nil {
		return fmt.Errorf("failed to load worker configuration: %w", err)
	}

	w.logger.WithFields(logrus.Fields{
		"network":            workerConfig.Network,
		"active_currencies":  len(workerConfig.ActiveCurrencies),
		"watched_addresses":  len(workerConfig.WatchedAddresses),
		"window_size":        w.windowSize,
	}).Info("Backfill worker configuration loaded")

	// Main processing loop
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Backfill worker stopped")
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
					"window_size":        w.windowSize,
				}).Info("✅ Configuration reloaded successfully")
			}

		case <-ticker.C:
			if err := w.processOnce(ctx, workerConfig); err != nil {
				w.logger.WithError(err).Error("Error in backfill worker iteration")
			}
		}
	}
}

// TriggerConfigReload signals the worker to reload its configuration
func (w *BackfillWorker) TriggerConfigReload() {
	select {
	case w.reloadChannel <- true:
		w.logger.Debug("Config reload signal sent")
	default:
		w.logger.Debug("Config reload already pending")
	}
}

// processOnce performs a single backfill worker iteration
func (w *BackfillWorker) processOnce(ctx context.Context, workerConfig *types.WorkerConfig) error {
	// Get starting block - simple approach
	startingBlock, err := w.findStartingBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to find starting block: %w", err)
	}

	// Use fixed 1000-block chunks for consistent processing
	chunkSize := int64(1000)
	fromBlock := startingBlock
	toBlock := startingBlock + chunkSize - 1

	// Simple stopping condition: stop when too close to current block
	currentBlock, err := w.rpcClient.GetBlockNumber(ctx)
	if err == nil {
		// Stay behind current block by at least 50 blocks to avoid race conditions
		maxBlock := currentBlock - 50
		if fromBlock > maxBlock {
			w.logger.WithFields(logrus.Fields{
				"starting_block": startingBlock,
				"current_block": currentBlock,
				"safe_to_process_up_to": maxBlock,
			}).Info("Backfill caught up to recent blocks, stopping")
			return nil
		}

		// Adjust range if needed
		if toBlock > maxBlock {
			toBlock = maxBlock
			w.logger.WithFields(logrus.Fields{
				"adjusted_range": fmt.Sprintf("%d-%d", fromBlock, toBlock),
			}).Debug("Adjusted range to stay behind current block")
		}
	} else {
		w.logger.WithError(err).Warn("Failed to get current block number, proceeding without safety check")
	}

	w.logger.WithFields(logrus.Fields{
		"from_block": fromBlock,
		"to_block":   toBlock,
		"chunk_size": chunkSize,
	}).Info("Processing backfill chunk")

	// Process the block range - simplified without gap detection
	return w.processRange(ctx, workerConfig, fromBlock, toBlock)
}

// processRange processes a specific block range
func (w *BackfillWorker) processRange(ctx context.Context, workerConfig *types.WorkerConfig, fromBlock, toBlock int64) error {
	// Build token addresses for filtering
	var tokenAddresses []string
	for _, currency := range workerConfig.ActiveCurrencies {
		if !currency.IsNative {
			tokenAddresses = append(tokenAddresses, currency.ContractAddress)
		}
	}

	// Build topics filter (Transfer events to our watched addresses)
	topics := rpc.BuildLogFilter(tokenAddresses, workerConfig.WatchedAddresses)

	// Split large block ranges into smaller chunks (max 1000 blocks per call)
	blockChunkSize := int64(1000) // Safe for Alchemy/ERPC
	if toBlock-fromBlock+1 > blockChunkSize {
		w.logger.WithFields(logrus.Fields{
			"from_block": fromBlock,
			"to_block": toBlock,
			"total_range": toBlock - fromBlock + 1,
			"chunk_size": blockChunkSize,
		}).Info("Splitting large block range into chunks")
	}

	var allLogs []*types.ERC20Log
	for chunkStart := fromBlock; chunkStart <= toBlock; chunkStart += blockChunkSize {
		chunkEnd := chunkStart + blockChunkSize - 1
		if chunkEnd > toBlock {
			chunkEnd = toBlock
		}

		w.logger.WithFields(logrus.Fields{
			"chunk_start": chunkStart,
			"chunk_end": chunkEnd,
			"chunk_size": chunkEnd - chunkStart + 1,
		}).Debug("Processing block chunk")

		chunkLogs, err := w.rpcClient.GetLogs(ctx, chunkStart, chunkEnd, tokenAddresses, topics)
		if err != nil {
			return fmt.Errorf("failed to get logs for chunk %d-%d: %w", chunkStart, chunkEnd, err)
		}

		allLogs = append(allLogs, chunkLogs...)
	}

	logs := allLogs

	w.logger.WithFields(logrus.Fields{
		"from_block":     fromBlock,
		"to_block":       toBlock,
		"logs_found":    len(logs),
	}).Debug("Retrieved logs from RPC")

	// Decode and process events
	currencies := convertCurrencySliceToPointers(workerConfig.ActiveCurrencies)
	events, err := w.decoder.DecodeBatchLogs(logs, currencies, workerConfig.WatchedAddresses, workerConfig.ChainID)
	if err != nil {
		return fmt.Errorf("failed to decode logs: %w", err)
	}

	w.logger.WithFields(logrus.Fields{
		"from_block":     fromBlock,
		"to_block":       toBlock,
		"events_found":   len(events),
	}).Info("Decoded payment events")

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

	// Update cursor in Redis
	if err := w.redis.SetCursor(ctx, w.chainID, w.workerType, toBlock); err != nil {
		w.logger.WithError(err).Warn("Failed to update backfill cursor in Redis")
	}

	// Mark blocks as processed to avoid reprocessing
	for blockNum := fromBlock; blockNum <= toBlock; blockNum++ {
		// Mark in database for persistent tracking
		if err := w.db.MarkBlockProcessed(ctx, w.chainID, blockNum, "backfill"); err != nil {
			w.logger.WithError(err).WithField("block_number", blockNum).Warn("Failed to mark block as processed in database")
		}
	}

	// Persist cursor to database
	if err := w.db.UpsertCursor(ctx, w.chainID, w.workerType, toBlock); err != nil {
		w.logger.WithError(err).Warn("Failed to persist backfill cursor to database")
	}

	// Log progress
	w.logger.WithFields(logrus.Fields{
		"from_block":     fromBlock,
		"to_block":       toBlock,
		"events_found":   len(events),
	}).Info("Backfill range completed")

	return nil
}

// findStartingBlock finds the first block to start processing from
func (w *BackfillWorker) findStartingBlock(ctx context.Context) (int64, error) {
	// Find the FIRST gap across ALL worker types, not just backfill
	query := fmt.Sprintf(`
		WITH all_processed_blocks AS (
			SELECT DISTINCT block_number
			FROM %s.indexer_processed_blocks
			WHERE chain_id = $1
		),
		gaps AS (
			SELECT current.block_number + 1 as gap_start
			FROM all_processed_blocks current
			WHERE NOT EXISTS (
				SELECT 1 FROM all_processed_blocks next
				WHERE next.block_number = current.block_number + 1
			)
			ORDER BY current.block_number
			LIMIT 1
		)
		SELECT COALESCE(
			(SELECT gap_start FROM gaps),
			1  -- Fallback to block 1 if no gaps found and no blocks processed
		)
	`, w.db.IndexerSchema)

	var startingBlock int64
	err := w.db.IndexerPool.QueryRow(ctx, query, w.chainID).Scan(&startingBlock)
	if err != nil {
		w.logger.WithError(err).Error("Failed to find starting block, using fallback")
		// Fallback to block 1 if database query fails
		return 1, nil
	}

	w.logger.WithFields(logrus.Fields{
		"starting_block": startingBlock,
		"chain_id": w.chainID,
		"gap_detection": "all_workers",
	}).Info("Found backfill starting point - filling first gap")

	return startingBlock, nil
}


// processEvent handles a single payment event
func (w *BackfillWorker) processEvent(ctx context.Context, event *types.PaymentEvent, workerConfig *types.WorkerConfig) error {
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

	// Send "detected" webhook (for historical events)
	if err := w.webhook.SendPaymentWebhook(ctx, event, types.StatusDetected, 5); err != nil {
		return fmt.Errorf("failed to send detected webhook: %w", err)
	}

	// Schedule confirmation check for backfill events too
	if currentBlock, err := w.rpcClient.GetBlockNumber(ctx); err == nil {
		targetBlock := event.BlockNumber + int64(workerConfig.TargetConfirmations)
		targetTime := time.Now().Add(time.Duration(workerConfig.TargetConfirmations) * 15 * time.Second) // Approximate 15s per block

		w.logger.WithFields(logrus.Fields{
			"event_id":     event.ID,
			"current_block": currentBlock,
			"target_block":  targetBlock,
			"confirmations": workerConfig.TargetConfirmations,
		}).Debug("Scheduling confirmation check for backfill event")

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
	event.Confirmations = 5 // Minimum confirmations
	event.FirstDetectedAt = &[]time.Time{time.Now()}[0]

	return nil
}


// GetSafeStartBlock calculates a safe starting block number
func (w *BackfillWorker) GetSafeStartBlock(ctx context.Context) (int64, error) {
	// Get current block number
	currentBlock, err := w.rpcClient.GetBlockNumber(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get current block number: %w", err)
	}

	// Start from current block - 100 as safety buffer
	startBlock := currentBlock - 100
	if startBlock < 0 {
		startBlock = 0
	}

	return startBlock, nil
}

// Health checks the health of the backfill worker
func (w *BackfillWorker) Health(ctx context.Context) error {
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
func (w *BackfillWorker) GetStatus(ctx context.Context) (*types.WorkerStatus, error) {
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
func (w *BackfillWorker) Stop() error {
	w.logger.Info("Stopping backfill worker")
	// Cleanup would go here if needed
	return nil
}