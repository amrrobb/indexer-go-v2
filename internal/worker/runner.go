package worker

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"indexer-go-v2/internal/config"
	"indexer-go-v2/internal/database"
	"indexer-go-v2/internal/redis"
	"indexer-go-v2/internal/rpc"
	"indexer-go-v2/internal/webhook"
)

// WorkerType represents the type of worker to run
type WorkerType string

const (
	ForwardType     WorkerType = "forward"
	BackfillType    WorkerType = "backfill"
	ConfirmationType WorkerType = "confirmation"
)

// WorkerRunner handles common initialization and running logic for all workers
type WorkerRunner struct {
	workerType WorkerType
	logger     *logrus.Entry
}

// NewWorkerRunner creates a new worker runner
func NewWorkerRunner(workerType WorkerType) *WorkerRunner {
	return &WorkerRunner{
		workerType: workerType,
		logger: logrus.WithFields(logrus.Fields{
			"component": "worker_runner",
			"worker_type": workerType,
		}),
	}
}

// Run initializes and runs the specified worker with the given chain ID
func (r *WorkerRunner) Run(chainID int, createWorker func(*config.Loader, *database.Client, *redis.Client, *rpc.Client, *webhook.Client, int) (interface{}, error), startWorker func(context.Context, interface{}) error) error {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		r.logger.Warnf("Warning: Could not load .env file: %v", err)
	}

	// Initialize logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	logger.WithFields(logrus.Fields{
		"worker_type": r.workerType,
		"chain_id":    chainID,
	}).Info("Starting indexer worker")

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize components (shared across all workers)
	db, err := database.NewClient()
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize database")
	}
	defer db.Close()

	redisClient, err := redis.NewClient(os.Getenv("REDIS_SENTINEL_URL"))
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize Redis")
	}
	defer redisClient.Close()

	// Load single-chain configuration
	configLoader := config.NewLoader(db)
	workerConfig, err := configLoader.LoadChainConfiguration(ctx, chainID)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load worker configuration")
	}

	// Cache the loaded configuration so the worker can find it
	configLoader.CacheWorkerConfig(chainID, workerConfig)

	// Initialize RPC client
	rpcClient := rpc.NewClient(workerConfig.RPCURL, "")

	// Initialize webhook client
	webhookClient, err := webhook.NewClient()
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize webhook client")
	}

	// Create the specific worker
	worker, err := createWorker(configLoader, db, redisClient, rpcClient, webhookClient, chainID)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create worker")
	}

	logger.Info("Worker initialized successfully")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start worker in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := startWorker(ctx, worker); err != nil {
			errChan <- fmt.Errorf("worker error: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		logger.WithField("signal", sig).Info("Received shutdown signal")
		cancel()

		// Give worker time to cleanup
		_, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		// Try to stop worker gracefully if it supports it
		if stopper, ok := worker.(interface{ Stop() error }); ok {
			if err := stopper.Stop(); err != nil {
				logger.WithError(err).Warn("Error during worker shutdown")
			}
		}

		select {
		case <-time.After(25 * time.Second):
			logger.Warn("Shutdown timeout reached")
		case <-errChan:
			logger.Info("Worker stopped gracefully")
		}

		logger.Info("Worker shutdown complete")

	case err := <-errChan:
		logger.WithError(err).Error("Worker failed unexpectedly")
		return err
	}

	return nil
}

// RunWorker is a convenience function that runs a worker with the given type
func RunWorker(workerType WorkerType) error {
	// Parse command line flags
	var chainID int
	flag.IntVar(&chainID, "chain", 1, "Chain ID to index (default: 1 for Ethereum)")
	flag.Parse()

	runner := NewWorkerRunner(workerType)

	switch workerType {
	case ForwardType:
		return runner.Run(chainID, func(configLoader *config.Loader, db *database.Client, redisClient *redis.Client, rpcClient *rpc.Client, webhookClient *webhook.Client, chainID int) (interface{}, error) {
			return NewForwardWorker(configLoader, db, redisClient, rpcClient, webhookClient, chainID)
		}, func(ctx context.Context, worker interface{}) error {
			return worker.(*ForwardWorker).Start(ctx)
		})

	case BackfillType:
		return runner.Run(chainID, func(configLoader *config.Loader, db *database.Client, redisClient *redis.Client, rpcClient *rpc.Client, webhookClient *webhook.Client, chainID int) (interface{}, error) {
			return NewBackfillWorker(configLoader, db, redisClient, rpcClient, webhookClient, chainID)
		}, func(ctx context.Context, worker interface{}) error {
			return worker.(*BackfillWorker).Start(ctx)
		})

	case ConfirmationType:
		return runner.Run(chainID, func(configLoader *config.Loader, db *database.Client, redisClient *redis.Client, rpcClient *rpc.Client, webhookClient *webhook.Client, chainID int) (interface{}, error) {
			return NewConfirmationWorker(configLoader, db, redisClient, rpcClient, webhookClient, chainID)
		}, func(ctx context.Context, worker interface{}) error {
			return worker.(*ConfirmationWorker).Start(ctx)
		})

	default:
		return fmt.Errorf("unknown worker type: %s", workerType)
	}
}