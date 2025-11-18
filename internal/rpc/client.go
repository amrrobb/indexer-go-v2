package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"indexer-go-v2/pkg/types"
)

// BatchConfig defines batching behavior
type BatchConfig struct {
	MaxAddressesPerCall int
	MaxConcurrentCalls  int
}

// Client represents an RPC client for blockchain interaction
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	logger      *logrus.Entry
	batchConfig BatchConfig
}

// NewClient creates a new RPC client with optimized batching for ERPC
func NewClient(baseURL, apiKey string) *Client {
	logger := logrus.WithField("component", "rpc_client")

	// Configurable timeouts
	httpTimeout := getEnvDuration("RPC_HTTP_TIMEOUT", 45*time.Second)
	dialTimeout := getEnvDuration("RPC_DIAL_TIMEOUT", 10*time.Second)
	headerTimeout := getEnvDuration("RPC_HEADER_TIMEOUT", 30*time.Second)

	logger.WithFields(logrus.Fields{
		"http_timeout":  httpTimeout,
		"dial_timeout":   dialTimeout,
		"header_timeout": headerTimeout,
	}).Info("RPC client configured with timeouts")

	return &Client{
		httpClient: &http.Client{
			Timeout: httpTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        20,  // Increased for better connection reuse
				IdleConnTimeout:     60 * time.Second,
				DisableCompression: false,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					dialer := &net.Dialer{
						Timeout: dialTimeout,
					}
					return dialer.DialContext(ctx, network, addr)
				},
				ResponseHeaderTimeout: headerTimeout,
			},
		},
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		logger:  logger,
		batchConfig: BatchConfig{
			MaxAddressesPerCall: 100, // Optimized for paid Alchemy via ERPC
			MaxConcurrentCalls:  5,   // ERPC handles concurrency well
		},
	}
}

// getEnvDuration gets a duration from environment variable or returns default
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// GetBlockNumber gets the latest block number with retry logic
func (c *Client) GetBlockNumber(ctx context.Context) (int64, error) {
	var lastErr error
	maxRetries := 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := time.Duration(attempt) * time.Second
			c.logger.WithFields(logrus.Fields{
				"attempt":      attempt,
				"max_retries":  maxRetries,
				"delay":        delay,
			}).Warn("Retrying get block number")

			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(delay):
				// Continue with retry
			}
		}

		request := &types.RPCRequest{
			ID:      "1",
			JsonRPC: "2.0",
			Method:  "eth_blockNumber",
			Params:  []interface{}{},
		}

		response, err := c.call(ctx, request)
		if err != nil {
			lastErr = err
			c.logger.WithFields(logrus.Fields{
				"attempt": attempt + 1,
				"error":   err.Error(),
			}).Warn("Failed to get block number")
			continue
		}

		if response.Result == nil {
			lastErr = fmt.Errorf("no result in block number response")
			continue
		}

		resultHex, ok := response.Result.(string)
		if !ok {
			lastErr = fmt.Errorf("invalid block number result type")
			continue
		}

		blockNumber, err := strconv.ParseInt(strings.TrimPrefix(resultHex, "0x"), 16, 64)
		if err != nil {
			lastErr = fmt.Errorf("failed to parse block number: %w", err)
			continue
		}

		// Success
		if attempt > 0 {
			c.logger.WithField("attempt", attempt+1).Info("Successfully got block number after retry")
		}
		return blockNumber, nil
	}

	return 0, fmt.Errorf("failed to get block number after %d attempts: %w", maxRetries+1, lastErr)
}

// GetLogs performs eth_getLogs with filtering and retry logic
func (c *Client) GetLogs(ctx context.Context, fromBlock, toBlock int64, addresses []string, topics [][]string) ([]*types.ERC20Log, error) {
	var lastErr error
	maxRetries := 2 // Fewer retries for GetLogs to avoid delays

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 2 * time.Second
			c.logger.WithFields(logrus.Fields{
				"attempt":      attempt,
				"max_retries":  maxRetries,
				"from_block":   fromBlock,
				"to_block":     toBlock,
				"delay":        delay,
			}).Warn("Retrying get logs")

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				// Continue with retry
			}
		}

		logs, err := c.getLogsAttempt(ctx, fromBlock, toBlock, addresses, topics)
		if err != nil {
			lastErr = err
			c.logger.WithFields(logrus.Fields{
				"attempt":    attempt + 1,
				"from_block": fromBlock,
				"to_block":   toBlock,
				"error":      err.Error(),
			}).Warn("Failed to get logs")
			continue
		}

		// Success
		if attempt > 0 {
			c.logger.WithFields(logrus.Fields{
				"attempt":    attempt + 1,
				"from_block": fromBlock,
				"to_block":   toBlock,
				"logs":       len(logs),
			}).Info("Successfully got logs after retry")
		}
		return logs, nil
	}

	return nil, fmt.Errorf("failed to get logs after %d attempts: %w", maxRetries+1, lastErr)
}

// getLogsAttempt performs a single eth_getLogs attempt
func (c *Client) getLogsAttempt(ctx context.Context, fromBlock, toBlock int64, addresses []string, topics [][]string) ([]*types.ERC20Log, error) {
	// Convert block numbers to hex
	fromHex := fmt.Sprintf("0x%x", fromBlock)
	toHex := fmt.Sprintf("0x%x", toBlock)

	// Prepare parameters
	params := map[string]interface{}{
		"fromBlock": fromHex,
		"toBlock":   toHex,
		"topics":    topics,
	}

	// Add addresses if provided
	if len(addresses) > 0 {
		params["address"] = addresses
	}

	request := &types.RPCRequest{
		ID:      "1",
		JsonRPC: "2.0",
		Method:  "eth_getLogs",
		Params:  []interface{}{params},
	}

	response, err := c.call(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	if response.Result == nil {
		return nil, fmt.Errorf("no result in logs response")
	}

	// Parse logs array
	logsArray, ok := response.Result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid logs result type")
	}

	var logs []*types.ERC20Log
	for i, logInterface := range logsArray {
		logBytes, err := json.Marshal(logInterface)
		if err != nil {
			c.logger.WithError(err).WithField("log_index", i).Warn("Failed to marshal log")
			continue
		}

		var log types.ERC20Log
		if err := json.Unmarshal(logBytes, &log); err != nil {
			c.logger.WithError(err).WithField("log_index", i).Warn("Failed to unmarshal log")
			continue
		}

		logs = append(logs, &log)
	}

	return logs, nil
}

// GetLogsBached handles large numbers of addresses by batching them automatically
// This method transparently splits addresses into optimal batches for ERPC
func (c *Client) GetLogsBatched(ctx context.Context, fromBlock, toBlock int64, addresses []string, topics [][]string) ([]*types.ERC20Log, error) {
	// If addresses are within limit, use regular GetLogs
	if len(addresses) <= c.batchConfig.MaxAddressesPerCall {
		c.logger.WithFields(logrus.Fields{
			"from_block": fromBlock,
			"to_block":   toBlock,
			"addresses":  len(addresses),
			"batched":    false,
		}).Debug("Getting logs without batching")
		return c.GetLogs(ctx, fromBlock, toBlock, addresses, topics)
	}

	// Split addresses into batches
	batches := c.chunkAddresses(addresses)
	c.logger.WithFields(logrus.Fields{
		"from_block":   fromBlock,
		"to_block":     toBlock,
		"total_addresses": len(addresses),
		"batch_count":  len(batches),
		"batch_size":   c.batchConfig.MaxAddressesPerCall,
	}).Info("Getting logs with address batching")

	// Process batches in parallel with concurrency limit
	allLogs := c.processBatchesInParallel(ctx, fromBlock, toBlock, batches, topics)

	c.logger.WithField("total_logs", len(allLogs)).Debug("Batched logs request completed")
	return allLogs, nil
}

// chunkAddresses splits addresses into optimal batch sizes
func (c *Client) chunkAddresses(addresses []string) [][]string {
	var batches [][]string
	batchSize := c.batchConfig.MaxAddressesPerCall

	for i := 0; i < len(addresses); i += batchSize {
		end := i + batchSize
		if end > len(addresses) {
			end = len(addresses)
		}
		batches = append(batches, addresses[i:end])
	}

	return batches
}

// processBatchesInParallel processes address batches with concurrency control
func (c *Client) processBatchesInParallel(ctx context.Context, fromBlock, toBlock int64, batches [][]string, topics [][]string) []*types.ERC20Log {
	type result struct {
		logs []*types.ERC20Log
		err  error
	}

	resultChan := make(chan result, len(batches))
	semaphore := make(chan struct{}, c.batchConfig.MaxConcurrentCalls)

	// Start goroutines for each batch
	for i, batch := range batches {
		go func(batchIndex int, addressBatch []string) {
			// Acquire semaphore for concurrency control
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			c.logger.WithFields(logrus.Fields{
				"batch_id":   batchIndex + 1,
				"total_batches": len(batches),
				"addresses":  len(addressBatch),
			}).Debug("Processing address batch")

			logs, err := c.GetLogs(ctx, fromBlock, toBlock, addressBatch, topics)
			resultChan <- result{logs: logs, err: err}
		}(i, batch)
	}

	// Collect results
	var allLogs []*types.ERC20Log
	var errors []error

	for i := 0; i < len(batches); i++ {
		select {
		case res := <-resultChan:
			if res.err != nil {
				errors = append(errors, res.err)
				c.logger.WithError(res.err).Warn("Address batch failed")
				continue
			}
			allLogs = append(allLogs, res.logs...)
		case <-ctx.Done():
			c.logger.Warn("Batch processing cancelled")
			return allLogs
		}
	}

	if len(errors) > 0 {
		c.logger.WithField("errors", len(errors)).Warn("Some address batches failed")
	}

	return allLogs
}

// GetBatchLogs performs multiple eth_getLogs calls in parallel for efficiency
func (c *Client) GetBatchLogs(ctx context.Context, requests []*LogRequest) ([]*types.ERC20Log, error) {
	type result struct {
		logs []*types.ERC20Log
		err  error
	}

	resultChan := make(chan result, len(requests))
	semaphore := make(chan struct{}, 5) // Limit concurrent requests

	// Process requests in parallel
	for _, req := range requests {
		go func(lr *LogRequest) {
			semaphore <- struct{}{} // Acquire
			defer func() { <-semaphore }() // Release

			logs, err := c.GetLogs(ctx, lr.FromBlock, lr.ToBlock, lr.Addresses, lr.Topics)
			resultChan <- result{logs: logs, err: err}
		}(req)
	}

	// Collect results
	var allLogs []*types.ERC20Log
	var errors []error

	for i := 0; i < len(requests); i++ {
		res := <-resultChan
		if res.err != nil {
			errors = append(errors, res.err)
			c.logger.WithError(res.err).Warn("Batch log request failed")
			continue
		}
		allLogs = append(allLogs, res.logs...)
	}

	if len(errors) > 0 {
		c.logger.WithField("errors", len(errors)).Warn("Some batch requests failed")
	}

	return allLogs, nil
}

// GetBlockWithRetry gets a block with retry logic
func (c *Client) GetBlockWithRetry(ctx context.Context, blockNumber int64, maxRetries int) (*types.Block, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
				c.logger.WithFields(logrus.Fields{
					"block_number": blockNumber,
					"attempt":      attempt,
					"max_retries":  maxRetries,
				}).Warn("Retrying block request")
			}
		}

		block, err := c.GetBlock(ctx, blockNumber)
		if err == nil {
			return block, nil
		}

		lastErr = err
		c.logger.WithFields(logrus.Fields{
			"block_number": blockNumber,
			"attempt":      attempt + 1,
			"error":        err.Error(),
		}).Warn("Block request failed")
	}

	return nil, fmt.Errorf("failed to get block %d after %d attempts: %w", blockNumber, maxRetries, lastErr)
}

// GetBlock retrieves block information
func (c *Client) GetBlock(ctx context.Context, blockNumber int64) (*types.Block, error) {
	blockHex := fmt.Sprintf("0x%x", blockNumber)

	request := &types.RPCRequest{
		ID:      "1",
		JsonRPC: "2.0",
		Method:  "eth_getBlockByNumber",
		Params:  []interface{}{blockHex, false}, // false = return full transaction objects
	}

	response, err := c.call(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	if response.Result == nil {
		return nil, fmt.Errorf("no result in block response")
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block result: %w", err)
	}

	var block types.Block
	if err := json.Unmarshal(resultBytes, &block); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	return &block, nil
}

// GetTransactionReceipt gets transaction receipt
func (c *Client) GetTransactionReceipt(ctx context.Context, txHash string) (*types.Receipt, error) {
	request := &types.RPCRequest{
		ID:      "1",
		JsonRPC: "2.0",
		Method:  "eth_getTransactionReceipt",
		Params:  []interface{}{txHash},
	}

	response, err := c.call(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	if response.Result == nil {
		return nil, nil // Transaction might not be mined yet
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal receipt result: %w", err)
	}

	var receipt types.Receipt
	if err := json.Unmarshal(resultBytes, &receipt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal receipt: %w", err)
	}

	return &receipt, nil
}

// BuildLogFilter creates a filter for ERC20 Transfer events
// Note: We don't filter by "to" address in topics due to RPC limitations.
// Instead, we filter by Transfer event signature and handle "to" address filtering in decoding.
func BuildLogFilter(tokenAddresses []string, watchedAddresses []string) [][]string {
	topics := make([][]string, 3)

	// Topic 0: Transfer event signature
	topics[0] = []string{types.TransferEventSignature}

	// Topic 1: From address (any)
	topics[1] = nil

	// Topic 2: To address (any - we'll filter in decoding)
	// Note: Many RPC endpoints don't support multiple addresses in a single topic position
	topics[2] = nil

	return topics
}

// AdaptWindowSize adjusts window size based on RPC response
func AdaptWindowSize(currentSize int, err error) int {
	if err == nil {
		// Success, can try larger windows (up to a limit)
		if currentSize < 10000 {
			return min(currentSize*2, 10000)
		}
		return currentSize
	}

	// Error, reduce window size
	if strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "request timeout") ||
		strings.Contains(err.Error(), "too many results") {
		return max(currentSize/2, 100) // Don't go below 100
	}

	// Other errors, keep same size but log
	return currentSize
}

// Call is a public method for making raw RPC calls (used for testing)
func (c *Client) Call(ctx context.Context, request *types.RPCRequest) (*types.RPCResponse, error) {
	return c.call(ctx, request)
}

// call makes an RPC call to the blockchain node
func (c *Client) call(ctx context.Context, request *types.RPCRequest) (*types.RPCResponse, error) {
	// Prepare request body
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "indexer-go-v2/1.0")

	// Add API key if provided
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("RPC request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse response
	var response types.RPCResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check for RPC error
	if response.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", response.Error.Message)
	}

	return &response, nil
}

// LogRequest represents a request for logs
type LogRequest struct {
	FromBlock int64
	ToBlock   int64
	Addresses []string
	Topics    [][]string
}

// CreateLogRequests creates log requests for a range, splitting into appropriate chunks
func CreateLogRequests(fromBlock, toBlock int64, addresses []string, topics [][]string, chunkSize int64) []*LogRequest {
	var requests []*LogRequest

	for from := fromBlock; from <= toBlock; from += chunkSize {
		to := from + chunkSize - 1
		if to > toBlock {
			to = toBlock
		}

		requests = append(requests, &LogRequest{
			FromBlock: from,
			ToBlock:   to,
			Addresses: addresses,
			Topics:    topics,
		})
	}

	return requests
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}