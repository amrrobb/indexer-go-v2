package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
	"indexer-go-v2/internal/worker"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: Could not load .env file: %v", err)
	}

	// Initialize logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	logger.WithField("component", "main").Info("Starting indexer confirmation worker")

	// Parse command line flags
	var chainID int
	flag.IntVar(&chainID, "chain", 1, "Chain ID to index (default: 1 for Ethereum)")
	flag.Parse()

	logger.WithField("chain_id", chainID).Info("Initializing confirmation worker")

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize components
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

	// Load configuration
	configLoader := config.NewLoader(db)

	// Load only the specific chain configuration we need
	workerConfig, err := configLoader.LoadChainConfiguration(ctx, chainID)
	if err != nil {
		logger.WithError(err).Fatal("Failed to get worker configuration")
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

	// Create confirmation worker
	confirmationWorker, err := worker.NewConfirmationWorker(
		configLoader,
		db,
		redisClient,
		rpcClient,
		webhookClient,
		chainID,
	)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create confirmation worker")
	}

	// Health check before starting
	if err := confirmationWorker.Health(ctx); err != nil {
		logger.WithError(err).Fatal("Worker health check failed")
	}

	logger.Info("Confirmation worker initialized successfully")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start worker in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := confirmationWorker.Start(ctx); err != nil {
			errChan <- fmt.Errorf("confirmation worker error: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		logger.WithField("signal", sig).Info("Received shutdown signal")

		// Cancel context to stop worker
		cancel()

		// Give worker time to cleanup
		_, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := confirmationWorker.Stop(); err != nil {
			logger.WithError(err).Warn("Error during worker shutdown")
		}

		// Wait for graceful shutdown or timeout
		select {
		case <-time.After(25 * time.Second):
			logger.Warn("Shutdown timeout reached")
		case <-errChan:
			logger.Info("Worker stopped gracefully")
		}

		logger.Info("Confirmation worker shutdown complete")

	case err := <-errChan:
		logger.WithError(err).Error("Worker failed unexpectedly")
		os.Exit(1)
	}
}