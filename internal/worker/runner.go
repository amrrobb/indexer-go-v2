package worker

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	// Start health check server
	healthServer := &healthServer{
		port:   getEnvAsInt("HEALTH_CHECK_PORT", 8080),
		logger: logger.WithField("component", "health_server"),
	}
	healthServer.start()
	defer healthServer.stop(ctx)

	// Start config change listener if enabled
	if os.Getenv("CONFIG_AUTO_RELOAD") == "true" {
		logger.Info("Config auto-reload is ENABLED - will reload on database changes")

		listener := config.NewConfigChangeListener(db.ConfigPool, configLoader)

		// Start listener in goroutine
		go func() {
			if err := listener.Start(ctx); err != nil {
				if err != context.Canceled {
					logger.WithError(err).Error("Config listener stopped with error")
				}
			}
		}()

		// Subscribe to config change events
		configChanges := listener.GetReloadChannel()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case notification := <-configChanges:
					logger.WithFields(logrus.Fields{
						"table":      notification.Table,
						"operation":  notification.Operation,
						"network_id": notification.NetworkID,
					}).Info("📡 Config change detected - triggering worker reload")

					// Trigger config reload based on worker type
					if reloader, ok := worker.(interface{ TriggerConfigReload() }); ok {
						reloader.TriggerConfigReload()
					} else {
						logger.Warn("Worker does not support config reload")
					}
				}
			}
		}()
	} else {
		logger.Warn("Config auto-reload is DISABLED - workers must be restarted to detect wallet/currency changes")
	}

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

// healthServer provides a simple HTTP health check endpoint for workers
type healthServer struct {
	server *http.Server
	logger *logrus.Entry
	port   int
}

func (h *healthServer) start() {
	mux := http.NewServeMux()

	// Simple health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Root endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Indexer Worker Running"))
	})

	h.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", h.port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		h.logger.WithField("port", h.port).Info("Health check server starting")
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.logger.WithError(err).Error("Health check server failed")
		}
	}()
}

func (h *healthServer) stop(ctx context.Context) {
	if h.server != nil {
		h.logger.Info("Shutting down health check server")
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		h.server.Shutdown(shutdownCtx)
	}
}

// getEnvAsInt reads an environment variable as integer with a fallback default
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}