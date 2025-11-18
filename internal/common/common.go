package common

import (
	"os"

	"github.com/sirupsen/logrus"
	"indexer-go-v2/internal/database"
	"indexer-go-v2/internal/redis"
	"indexer-go-v2/internal/rpc"
	"indexer-go-v2/internal/webhook"
)

// Components holds all initialized components needed by workers
type Components struct {
	DB           *database.Client
	Redis        *redis.Client
	RPCClient    *rpc.Client
	Webhook      *webhook.Client
	Logger       *logrus.Entry
}

// InitializeComponents sets up all common components (DB, Redis, RPC, Webhook)
func InitializeComponents(workerType string) (*Components, error) {
	logger := logrus.WithFields(logrus.Fields{
		"component":  "common",
		"worker_type": workerType,
	})

	logger.Info("Initializing common components")

	// Initialize database
	db, err := database.NewClient()
	if err != nil {
		logger.WithError(err).Error("Failed to initialize database")
		return nil, err
	}

	logger.Info("Database connection established successfully")

	// Initialize Redis
	redisClient, err := redis.NewClient(os.Getenv("REDIS_SENTINEL_URL"))
	if err != nil {
		logger.WithError(err).Error("Failed to initialize Redis")
		return nil, err
	}

	logger.Info("Redis connection established successfully")

	// Initialize webhook client
	webhookClient, err := webhook.NewClient()
	if err != nil {
		logger.WithError(err).Error("Failed to initialize webhook client")
		return nil, err
	}

	logger.Info("Webhook client initialized")

	return &Components{
		DB:        db,
		Redis:     redisClient,
		Webhook:   webhookClient,
		Logger:    logger,
	}, nil
}

// CloseComponents closes all components gracefully
func (c *Components) CloseComponents() {
	if c.Webhook != nil {
		// Webhook client doesn't have a close method typically
	}
	if c.Redis != nil {
		if err := c.Redis.Close(); err != nil {
			c.Logger.WithError(err).Error("Error closing Redis client")
		}
	}
	if c.DB != nil {
		if err := c.DB.Close(); err != nil {
			c.Logger.WithError(err).Error("Error closing database client")
		}
	}
	c.Logger.Info("All components closed")
}