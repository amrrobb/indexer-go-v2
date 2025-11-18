package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

// Client represents Redis client for state management
type Client struct {
	rdb    *redis.Client
	logger *logrus.Entry
}

// NewClient creates a new Redis client (backward compatibility)
func NewClient(redisURL string) (*Client, error) {
	// Check if Sentinel mode is enabled
	sentinelEnabled := os.Getenv("REDIS_SENTINEL") == "true"

	if sentinelEnabled {
		// Use new URL-based method for Sentinel
		sentinelURL := os.Getenv("REDIS_SENTINEL_URL")
		if sentinelURL != "" {
			return NewClientFromURL(sentinelURL)
		}

		// Fallback to old manual Sentinel configuration
		return NewClientFromManualConfig(redisURL)
	}

	// Direct connection mode
	return NewClientFromURL(redisURL)
}

// NewClientFromURL creates a new Redis client from URL (supports all Redis modes)
func NewClientFromURL(redisURL string) (*Client, error) {
	logger := logrus.WithField("component", "redis_client")

	if redisURL == "" {
		return nil, fmt.Errorf("Redis URL is required")
	}

	// Check if it's a Sentinel URL
	if strings.HasPrefix(redisURL, "redis+sentinel://") {
		return NewClientFromSentinelURL(redisURL)
	}

	// Parse regular Redis URL
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL %s: %w", redisURL, err)
	}

	logger.WithFields(logrus.Fields{
		"url": redisURL,
		"addr": opts.Addr,
	}).Info("Connecting to Redis via URL")

	// Create client
	rdb := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info("Redis connection established successfully")

	return &Client{
		rdb:    rdb,
		logger: logger,
	}, nil
}

// NewClientFromSentinelURL creates a Redis client from Sentinel URL
func NewClientFromSentinelURL(sentinelURL string) (*Client, error) {
	logger := logrus.WithField("component", "redis_client")

	// Parse Sentinel URL: redis+sentinel://password@sentinel1,sentinel2/mastername
	u, err := url.Parse(sentinelURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Sentinel URL %s: %w", sentinelURL, err)
	}

	// Extract master name from path
	masterName := strings.TrimPrefix(u.Path, "/")
	if masterName == "" {
		return nil, fmt.Errorf("master name is required in Sentinel URL")
	}

	// Extract password
	password := ""
	if u.User != nil {
		password = u.User.String()
	}

	// Split sentinel addresses
	sentinelAddrs := strings.Split(u.Host, ",")

	logger.WithFields(logrus.Fields{
		"sentinels":   sentinelAddrs,
		"master_name": masterName,
		"password":    password != "",
	}).Info("Connecting to Redis Sentinel")

	// Create Sentinel failover client
	rdb := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    masterName,
		SentinelAddrs: sentinelAddrs,
		Password:      password,
		DB:            0,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		// If the error is hostname resolution, try direct connection as fallback
		if strings.Contains(err.Error(), "lookup redis-master") {
			logger.Warn("Sentinel returned internal Docker hostname, falling back to direct Redis connection")
			// Use REDIS_URL from environment for fallback
			fallbackURL := os.Getenv("REDIS_URL")
			if fallbackURL == "" {
				return nil, fmt.Errorf("failed to connect to Redis Sentinel and REDIS_URL not set: %w", err)
			}
			return NewClientFromURL(fallbackURL)
		}
		return nil, fmt.Errorf("failed to connect to Redis Sentinel: %w", err)
	}

	logger.Info("Redis Sentinel connection established successfully")

	return &Client{
		rdb:    rdb,
		logger: logger,
	}, nil
}

// NewClientFromManualConfig creates Redis client using manual configuration (backward compatibility)
func NewClientFromManualConfig(redisURL string) (*Client, error) {
	logger := logrus.WithField("component", "redis_client")

	// Sentinel mode configuration
	sentinels := strings.Split(os.Getenv("REDIS_SENTINELS"), ",")
	masterName := os.Getenv("REDIS_SENTINEL_NAME")
	password := os.Getenv("REDIS_SENTINEL_PASSWORD")
	db, _ := strconv.Atoi(os.Getenv("REDIS_DB"))

	logger.WithFields(logrus.Fields{
		"sentinels":  sentinels,
		"master_name": masterName,
		"db": db,
	}).Info("Connecting to Redis in manual Sentinel mode")

	// Create failover client for Sentinel
	rdb := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    masterName,
		SentinelAddrs: sentinels,
		Password:      password,
		DB:            db,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis Sentinel: %w", err)
	}

	logger.Info("Redis Sentinel connection established successfully (manual config)")

	return &Client{
		rdb:    rdb,
		logger: logger,
	}, nil
}

// NewClientDirectMode creates a direct Redis connection (legacy support)
func NewClientDirectMode(redisURL string) (*Client, error) {
	logger := logrus.WithField("component", "redis_client")

	addr := redisURL
	password := os.Getenv("REDIS_PASSWORD")

	// Default to localhost:6379 if just "redis://hostname" is provided
	if strings.HasPrefix(redisURL, "redis://") {
		host := strings.TrimPrefix(redisURL, "redis://")
		if !strings.Contains(host, ":") {
			host += ":6379"
		}
		addr = host
	}

	logger.WithFields(logrus.Fields{
		"addr": addr,
	}).Info("Connecting to Redis in direct mode")

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info("Redis direct connection established successfully")

	return &Client{
		rdb:    rdb,
		logger: logger,
	}, nil
}

// Close closes the Redis connection
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Health checks Redis health
func (c *Client) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.rdb.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("Redis health check failed: %w", err)
	}
	return nil
}

// Cursor operations

// SetCursor sets the last processed block for a worker
func (c *Client) SetCursor(ctx context.Context, chainID int, workerType string, lastBlock int64) error {
	key := fmt.Sprintf("idx:%d:cursor:%s", chainID, workerType)
	return c.rdb.Set(ctx, key, lastBlock, 0).Err()
}

// GetCursor gets the last processed block for a worker
func (c *Client) GetCursor(ctx context.Context, chainID int, workerType string) (int64, error) {
	key := fmt.Sprintf("idx:%d:cursor:%s", chainID, workerType)
	result, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil // No cursor exists yet
		}
		return 0, fmt.Errorf("failed to get cursor: %w", err)
	}

	block, err := strconv.ParseInt(result, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cursor block number: %w", err)
	}

	return block, nil
}

// DeleteCursor removes a cursor entry
func (c *Client) DeleteCursor(ctx context.Context, chainID int, workerType string) error {
	key := fmt.Sprintf("idx:%d:cursor:%s", chainID, workerType)
	return c.rdb.Del(ctx, key).Err()
}

// Seen block operations

// MarkBlockSeen marks a block as processed to avoid overlap
func (c *Client) MarkBlockSeen(ctx context.Context, chainID int, blockNumber int64, ttl time.Duration) error {
	key := fmt.Sprintf("idx:%d:seen:block:%d", chainID, blockNumber)
	return c.rdb.Set(ctx, key, "1", ttl).Err()
}

// IsBlockSeen checks if a block has been processed
func (c *Client) IsBlockSeen(ctx context.Context, chainID int, blockNumber int64) (bool, error) {
	key := fmt.Sprintf("idx:%d:seen:block:%d", chainID, blockNumber)
	result, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check if block is seen: %w", err)
	}
	return result > 0, nil
}

// Event deduplication operations

// AddEvent adds an event ID to the deduplication set
func (c *Client) AddEvent(ctx context.Context, eventID string) error {
	return c.rdb.SAdd(ctx, "events:dedupe", eventID).Err()
}

// IsEventProcessed checks if an event has been processed
func (c *Client) IsEventProcessed(ctx context.Context, eventID string) (bool, error) {
	result, err := c.rdb.SIsMember(ctx, "events:dedupe", eventID).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check if event is processed: %w", err)
	}
	return result, nil
}

// CleanupOldEvents removes old event IDs from deduplication set
func (c *Client) CleanupOldEvents(ctx context.Context, batchSize int) (int64, error) {
	// Remove a batch of old events to keep the set from growing indefinitely
	// In production, this should be more sophisticated (e.g., based on timestamp)
	removed, err := c.rdb.SPopN(ctx, "events:dedupe", int64(batchSize)).Result()
	if err != nil {
		return 0, err
	}
	return int64(len(removed)), nil
}

// Configuration cache operations

// SetConfig stores configuration in Redis
func (c *Client) SetConfig(ctx context.Context, version int64, config *ConfigCache) error {
	// Serialize config to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Calculate hash for verification
	hash := fmt.Sprintf("%x", configJSON)

	pipe := c.rdb.Pipeline()
	pipe.Set(ctx, fmt.Sprintf("config:snapshot:%d", version), configJSON, 0)
	pipe.Set(ctx, fmt.Sprintf("config:hash:%d", version), hash, 0)
	pipe.Set(ctx, "config:active_version", version, 0)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to store config in Redis: %w", err)
	}

	c.logger.WithFields(logrus.Fields{
		"version": version,
		"hash":    hash,
	}).Info("Configuration stored in Redis")

	return nil
}

// GetConfig retrieves configuration from Redis
func (c *Client) GetConfig(ctx context.Context, version int64) (*ConfigCache, error) {
	key := fmt.Sprintf("config:snapshot:%d")
	result, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get config from Redis: %w", err)
	}

	var config ConfigCache
	if err := json.Unmarshal([]byte(result), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

// GetActiveVersion gets the active configuration version
func (c *Client) GetActiveVersion(ctx context.Context) (int64, error) {
	result, err := c.rdb.Get(ctx, "config:active_version").Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil // No version exists yet
		}
		return 0, fmt.Errorf("failed to get active version: %w", err)
	}

	version, err := strconv.ParseInt(result, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse version: %w", err)
	}

	return version, nil
}

// Confirmation scheduling operations

// ScheduleConfirmation schedules a confirmation check for an event
func (c *Client) ScheduleConfirmation(ctx context.Context, eventID string, targetBlock int64, targetTime time.Time) error {
	key := fmt.Sprintf("events:confirm:%s", eventID)

	// Store target block and time as JSON
	data := map[string]interface{}{
		"target_block": targetBlock,
		"target_time": targetTime.Unix(),
		"status":      "pending",
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal confirmation data: %w", err)
	}

	return c.rdb.Set(ctx, key, dataJSON, 0).Err()
}

// GetConfirmationData gets confirmation scheduling data
func (c *Client) GetConfirmationData(ctx context.Context, eventID string) (*ConfirmationData, error) {
	key := fmt.Sprintf("events:confirm:%s")
	result, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get confirmation data: %w", err)
	}

	var data ConfirmationData
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal confirmation data: %w", err)
	}

	return &data, nil
}

// RemoveConfirmation removes a confirmation entry
func (c *Client) RemoveConfirmation(ctx context.Context, eventID string) error {
	key := fmt.Sprintf("events:confirm:%s", eventID)
	return c.rdb.Del(ctx, key).Err()
}

// GetScheduledConfirmations gets pending confirmations for a specific chain
func (c *Client) GetScheduledConfirmations(ctx context.Context, chainID int) ([]string, error) {
	pattern := fmt.Sprintf("events:confirm:%d:*", chainID)
	keys, err := c.rdb.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list confirmation keys: %w", err)
	}

	var eventIDs []string
	for _, key := range keys {
		// Extract event ID from key
		parts := strings.Split(key, ":")
		if len(parts) >= 3 {
			eventIDs = append(eventIDs, parts[3])
		}
	}

	return eventIDs, nil
}

// ListPendingConfirmations lists all pending confirmation checks
func (c *Client) ListPendingConfirmations(ctx context.Context) ([]string, error) {
	pattern := "events:confirm:*"
	keys, err := c.rdb.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list confirmation keys: %w", err)
	}

	var eventIDs []string
	for _, key := range keys {
		// Extract event ID from key
		parts := strings.Split(key, ":")
		if len(parts) >= 3 {
			eventIDs = append(eventIDs, parts[2])
		}
	}

	return eventIDs, nil
}

// EventDetails represents stored event details for confirmations
type EventDetails struct {
	EventID           string    `json:"event_id"`
	ChainID           int       `json:"chain_id"`
	TransactionHash   string    `json:"transaction_hash"`
	BlockNumber       int64     `json:"block_number"`
	FromAddress       string    `json:"from_address"`
	ToAddress         string    `json:"to_address"`
	TokenAddress      string    `json:"token_address"`
	TokenSymbol       string    `json:"token_symbol	TokenName"`
	TokenName         string    `json:"token_name"`
	Amount            string    `json:"amount"`
	AmountFormatted   string    `json:"amount_formatted"`
	FirstDetectedAt   time.Time `json:"first_detected_at"`
}

// SetEventDetails stores event details for confirmation processing
func (c *Client) SetEventDetails(ctx context.Context, eventID string, details *EventDetails, ttl time.Duration) error {
	key := fmt.Sprintf("events:details:%s", eventID)
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("failed to marshal event details: %w", err)
	}

	return c.rdb.Set(ctx, key, detailsJSON, ttl).Err()
}

// GetEventDetails retrieves event details for confirmation processing
func (c *Client) GetEventDetails(ctx context.Context, eventID string) (*EventDetails, error) {
	key := fmt.Sprintf("events:details:%s", eventID)
	result, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Event details not found
		}
		return nil, fmt.Errorf("failed to get event details: %w", err)
	}

	var details EventDetails
	if err := json.Unmarshal([]byte(result), &details); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event details: %w", err)
	}

	return &details, nil
}

// IsConfirmationReady checks if a confirmation is ready based on block number
func (c *Client) IsConfirmationReady(ctx context.Context, eventID string, currentBlock int64) (bool, error) {
	key := fmt.Sprintf("events:confirm:%s", eventID)

	// Get confirmation data
	result, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil // No confirmation scheduled
		}
		return false, fmt.Errorf("failed to get confirmation data: %w", err)
	}

	var data ConfirmationData
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		return false, fmt.Errorf("failed to unmarshal confirmation data: %w", err)
	}

	// Check if target block has been reached
	return currentBlock >= data.TargetBlock, nil
}

// MarkEventConfirmed marks an event as confirmed
func (c *Client) MarkEventConfirmed(ctx context.Context, eventID string, confirmations int) error {
	key := fmt.Sprintf("events:confirmed:%s", eventID)

	data := map[string]interface{}{
		"confirmed_at": time.Now().Unix(),
		"confirmations": confirmations,
		"status": "confirmed",
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal confirmation data: %w", err)
	}

	// Store with longer TTL (e.g., 7 days)
	return c.rdb.Set(ctx, key, dataJSON, 7*24*time.Hour).Err()
}

// IsEventConfirmed checks if an event has been confirmed
func (c *Client) IsEventConfirmed(ctx context.Context, eventID string) (bool, error) {
	key := fmt.Sprintf("events:confirmed:%s", eventID)
	result, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check if event is confirmed: %w", err)
	}
	return result > 0, nil
}

// Worker coordination operations

// SetWorkerStatus sets status information for a worker
func (c *Client) SetWorkerStatus(ctx context.Context, chainID int, workerType string, status *WorkerStatus) error {
	key := fmt.Sprintf("workers:%d:%s:status", chainID, workerType)
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal worker status: %w", err)
	}

	return c.rdb.Set(ctx, key, statusJSON, 60*time.Second).Err() // 60 second TTL
}

// GetWorkerStatus gets status information for a worker
func (c *Client) GetWorkerStatus(ctx context.Context, chainID int, workerType string) (*WorkerStatus, error) {
	key := fmt.Sprintf("workers:%d:%s:status", chainID, workerType)
	result, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker status: %w", err)
	}

	var status WorkerStatus
	if err := json.Unmarshal([]byte(result), &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal worker status: %w", err)
	}

	return &status, nil
}

// Metrics and monitoring operations

// IncrementCounter increments a metric counter
func (c *Client) IncrementCounter(ctx context.Context, metricName string) (int64, error) {
	key := fmt.Sprintf("metrics:%s", metricName)
	return c.rdb.Incr(ctx, key).Result()
}

// SetGauge sets a gauge metric value
func (c *Client) SetGauge(ctx context.Context, metricName string, value float64) error {
	key := fmt.Sprintf("metrics:%s", metricName)
	return c.rdb.Set(ctx, key, value, 0).Err()
}

// Utility functions

// ExpireKeys sets TTL on multiple keys with a pattern
func (c *Client) ExpireKeys(ctx context.Context, pattern string, ttl time.Duration) (int64, error) {
	keys, err := c.rdb.Keys(ctx, pattern).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to list keys for expire: %w", err)
	}

	var expiredCount int64
	for _, key := range keys {
		if c.rdb.Expire(ctx, key, ttl).Err() == nil {
			expiredCount++
		}
	}

	return expiredCount, nil
}

// FlushAll clears all indexer-related keys (for testing)
func (c *Client) FlushAll(ctx context.Context) error {
	patterns := []string{
		"idx:*",
		"events:*",
		"workers:*",
		"config:*",
		"metrics:*",
	}

	for _, pattern := range patterns {
		keys, err := c.rdb.Keys(ctx, pattern).Result()
		if err != nil {
			return fmt.Errorf("failed to list keys for pattern %s: %w", pattern, err)
		}

		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("failed to delete keys for pattern %s: %w", pattern, err)
			}
		}
	}

	c.logger.Info("All indexer keys flushed from Redis")
	return nil
}

// ConfigCache represents cached configuration data
type ConfigCache struct {
	Version     int64                  `json:"version"`
	Chains      map[int]interface{}    `json:"chains"`
	Currencies  map[int][]interface{}  `json:"currencies"`
	Wallets     map[int][]string       `json:"wallets"`
	WorkerConfigs map[int]interface{}  `json:"worker_configs"`
	LastUpdate  time.Time              `json:"last_update"`
}

// ConfirmationData represents confirmation scheduling data
type ConfirmationData struct {
	TargetBlock int64   `json:"target_block"`
	TargetTime   int64   `json:"target_time"`
	Status       string  `json:"status"`
	CreatedAt    int64   `json:"created_at"`
}

// WorkerStatus represents worker status information
type WorkerStatus struct {
	ChainID        int       `json:"chain_id"`
	WorkerType     string    `json:"worker_type"`
	CurrentBlock   int64     `json:"current_block"`
	LastProcessed  int64     `json:"last_processed"`
	BlocksPerSec   float64   `json:"blocks_per_sec"`
	EventsPerSec   float64   `json:"events_per_sec"`
	LastActivity   time.Time `json:"last_activity"`
	IsHealthy      bool      `json:"is_healthy"`
	ErrorCount     int       `json:"error_count"`
	LastError      string    `json:"last_error,omitempty"`
}