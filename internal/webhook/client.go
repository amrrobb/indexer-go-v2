package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"indexer-go-v2/pkg/types"
)

// Client represents a webhook client for sending payment notifications
type Client struct {
	httpClient      *http.Client
	webhookURL      string
	webhookSecret   string
	apiKey          string
	logger          *logrus.Entry
	maxRetries      int
	retryDelay      time.Duration
	timeout         time.Duration
}

// NewClient creates a new webhook client
func NewClient() (*Client, error) {
	webhookURL := os.Getenv("WEBHOOK_URL")
	if webhookURL == "" {
		return nil, fmt.Errorf("WEBHOOK_URL environment variable is required")
	}

	webhookSecret := os.Getenv("INDEXER_WEBHOOK_SECRET")
	apiKey := os.Getenv("INDEXER_API_KEY")

	// Parse configuration
	maxRetries := 3
	if retries := os.Getenv("WEBHOOK_RETRIES"); retries != "" {
		if parsed, err := strconv.Atoi(retries); err == nil {
			maxRetries = parsed
		}
	}

	retryDelay := 5 * time.Second
	if delay := os.Getenv("WEBHOOK_RETRY_DELAY"); delay != "" {
		if parsed, err := time.ParseDuration(delay); err == nil {
			retryDelay = parsed
		}
	}

	timeout := 30 * time.Second
	if timeoutStr := os.Getenv("WEBHOOK_TIMEOUT"); timeoutStr != "" {
		if parsed, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = parsed
		}
	}

	logger := logrus.WithField("component", "webhook_client")
	logger.WithFields(logrus.Fields{
		"webhook_url": webhookURL,
		"max_retries": maxRetries,
		"retry_delay": retryDelay,
		"timeout":    timeout,
	}).Info("Webhook client initialized")

	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		webhookURL:    webhookURL,
		webhookSecret: webhookSecret,
		apiKey:        apiKey,
		logger:        logger,
		maxRetries:    maxRetries,
		retryDelay:    retryDelay,
		timeout:       timeout,
	}, nil
}

// SendPaymentWebhook sends a payment notification webhook
func (c *Client) SendPaymentWebhook(ctx context.Context, event *types.PaymentEvent, status string, confirmations int) error {
	// Create webhook payload
	payload := c.buildPayload(event, status, confirmations)

	// Marshal to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	// Send with retry logic
	err = c.sendWithRetry(ctx, jsonData, payload.TxHash)
	if err != nil {
		c.logger.WithFields(logrus.Fields{
			"event_id":  payload.TxHash,
			"tx_hash":   payload.TxHash,
			"error":     err.Error(),
		}).Error("Failed to send webhook")
		return err
	}

	c.logger.WithFields(logrus.Fields{
		"event_id":      payload.TxHash,
		"tx_hash":       payload.TxHash,
		"status":        payload.Status,
		"confirmations": payload.Confirmations,
		"token_symbol":  payload.TokenSymbol,
		"amount":        payload.AmountFormatted,
	}).Info("Webhook sent successfully")

	return nil
}

// SendConfirmationWebhook sends a confirmation webhook for an already detected payment
func (c *Client) SendConfirmationWebhook(ctx context.Context, event *types.PaymentEvent, targetConfirmations int) error {
	if event.Status == types.StatusConfirmed {
		// Already confirmed, no need to send again
		c.logger.WithFields(logrus.Fields{
			"event_id": event.ID,
			"tx_hash": event.TransactionHash,
		}).Debug("Payment already confirmed, skipping confirmation webhook")
		return nil
	}

	// Update event to confirmed status
	confirmedEvent := *event
	confirmedEvent.Status = types.StatusConfirmed
	confirmedEvent.Confirmations = targetConfirmations
	if confirmedEvent.FirstDetectedAt == nil {
		confirmedEvent.FirstDetectedAt = &event.Timestamp
	}

	return c.SendPaymentWebhook(ctx, &confirmedEvent, types.StatusConfirmed, targetConfirmations)
}

// buildPayload creates webhook payload from payment event
func (c *Client) buildPayload(event *types.PaymentEvent, status string, confirmations int) *types.WebhookPayload {
	// Determine chain name for backend compatibility
	chainName := types.GetChainName(event.ChainID)
	if chainName == "UNKNOWN" {
		c.logger.Warnf("Unknown chain ID %d, defaulting to ETHEREUM", event.ChainID)
		chainName = "ETHEREUM" // Default fallback
	} else {
		c.logger.Infof("Chain ID %d resolved to %s", event.ChainID, chainName)
	}

	// Note: Token type logic could be enhanced to check currency.IsNative if available

	// Get target confirmations based on chain
	targetConfirmations := confirmations
	if targetConfirmations == 0 {
		// Use default chain-specific confirmations
		if target, exists := types.ChainConfirmations[strings.ToLower(chainName)]; exists {
			targetConfirmations = target
		} else {
			targetConfirmations = 12 // Default to Ethereum
		}
	}

	payload := &types.WebhookPayload{
		TxHash:              event.TransactionHash,
		FromAddress:         event.FromAddress,
		ToAddress:           event.ToAddress,
		Amount:              event.Amount,
		AmountFormatted:     event.AmountFormatted,
		TokenAddress:        event.TokenAddress,
		Chain:               chainName,
		ChainID:             event.ChainID,
		BlockNumber:         uint64(event.BlockNumber),
		Status:              status,
		Confirmations:       confirmations,
		Timestamp:           time.Now(),
		TokenSymbol:         event.TokenSymbol,
		TokenName:           event.TokenName,
		CurrentConfirmations: confirmations,
		TargetConfirmations:  targetConfirmations,
	}

	// Add first detected at for confirmed payments
	if status == types.StatusConfirmed && event.FirstDetectedAt != nil {
		payload.FirstDetectedAt = event.FirstDetectedAt
	}

	return payload
}

// sendWithRetry sends webhook with retry logic
func (c *Client) sendWithRetry(ctx context.Context, jsonData []byte, eventID string) error {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryDelay * time.Duration(attempt)):
				c.logger.WithFields(logrus.Fields{
					"event_id": eventID,
					"attempt":  attempt,
					"delay":    c.retryDelay * time.Duration(attempt),
				}).Warn("Retrying webhook request")
			}
		}

		// Create HTTP request
		req, err := http.NewRequestWithContext(ctx, "POST", c.webhookURL, bytes.NewReader(jsonData))
		if err != nil {
			lastErr = fmt.Errorf("failed to create webhook request: %w", err)
			continue
		}

		// Set headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "indexer-go-v2/1.0")

		// Add API key authentication if provided
		if c.apiKey != "" {
			req.Header.Set("Authorization", "ApiKey "+c.apiKey)
		}

		// Add HMAC signature if secret is configured
		if c.webhookSecret != "" {
			signature := c.generateHMACSignature(jsonData)
			req.Header.Set("X-Webhook-Signature", signature)
		}

		// Send request
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send webhook request: %w", err)
			c.logger.WithFields(logrus.Fields{
				"event_id": eventID,
				"attempt":  attempt + 1,
				"error":    err.Error(),
			}).Warn("Webhook request failed")
			continue
		}

		// Check response status
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			return nil // Success
		}

		// Non-success response
		resp.Body.Close()
		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)

		c.logger.WithFields(logrus.Fields{
			"event_id":     eventID,
			"attempt":      attempt + 1,
			"status_code":  resp.StatusCode,
			"last_error":   lastErr.Error(),
		}).Warn("Webhook request returned non-success status")
	}

	return fmt.Errorf("webhook delivery failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

// generateHMACSignature creates HMAC-SHA256 signature for webhook payload
func (c *Client) generateHMACSignature(payload []byte) string {
	h := hmac.New(sha256.New, []byte(c.webhookSecret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// IsEnabled checks if webhook is properly configured
func (c *Client) IsEnabled() bool {
	return c.webhookURL != ""
}

// TestWebhook sends a test webhook to verify connectivity
func (c *Client) TestWebhook(ctx context.Context) error {
	if !c.IsEnabled() {
		return fmt.Errorf("webhook URL not configured")
	}

	testPayload := &types.WebhookPayload{
		TxHash:          "0x0000000000000000000000000000000000000000000000000000000000000000000",
		FromAddress:     "0x0000000000000000000000000000000000000000",
		ToAddress:       "0x0000000000000000000000000000000000000000",
		Amount:          "0",
		AmountFormatted: "0.000000",
		TokenAddress:    "0x0000000000000000000000000000000000000000",
		Chain:           "ETHEREUM",
		BlockNumber:     0,
		Status:          types.StatusDetected,
		Confirmations:   0,
		Timestamp:       time.Now(),
		TokenSymbol:     "TEST",
		TokenName:       "Test Token",
	}

	jsonData, err := json.Marshal(testPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal test webhook payload: %w", err)
	}

	c.logger.Info("Sending test webhook")
	return c.sendWithRetry(ctx, jsonData, "test-event")
}

// SendBatchWebhooks sends multiple webhooks in parallel
func (c *Client) SendBatchWebhooks(ctx context.Context, events []*types.WebhookPayload) error {
	if len(events) == 0 {
		return nil
	}

	// Limit concurrent requests to avoid overwhelming the webhook server
	semaphore := make(chan struct{}, 5)
	errChan := make(chan error, len(events))

	// Send webhooks in parallel
	for i, event := range events {
		go func(idx int, payload *types.WebhookPayload) {
			semaphore <- struct{}{} // Acquire
			defer func() { <-semaphore }() // Release

			jsonData, err := json.Marshal(payload)
			if err != nil {
				errChan <- fmt.Errorf("failed to marshal webhook payload %d: %w", idx, err)
				return
			}

			err = c.sendWithRetry(ctx, jsonData, payload.TxHash)
			errChan <- err
		}(i, event)
	}

	// Collect results
	var errors []error
	for i := 0; i < len(events); i++ {
		if err := <-errChan; err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		c.logger.WithFields(logrus.Fields{
			"total_events": len(events),
			"failed_events": len(errors),
		}).Warn("Some webhook batch requests failed")
		return fmt.Errorf("%d webhook requests failed", len(errors))
	}

	c.logger.WithFields(logrus.Fields{
		"total_events": len(events),
	}).Info("Successfully sent batch webhooks")

	return nil
}

// ValidateConfiguration validates webhook configuration
func (c *Client) ValidateConfiguration() error {
	if c.webhookURL == "" {
		return fmt.Errorf("WEBHOOK_URL is required")
	}

	if c.apiKey == "" && c.webhookSecret == "" {
		return fmt.Errorf("at least one authentication method (API key or webhook secret) must be configured")
	}

	if c.maxRetries < 0 || c.maxRetries > 10 {
		return fmt.Errorf("WEBHOOK_RETRIES must be between 0 and 10")
	}

	if c.retryDelay < 1*time.Second || c.retryDelay > 60*time.Second {
		return fmt.Errorf("WEBHOOK_RETRY_DELAY must be between 1s and 60s")
	}

	if c.timeout < 5*time.Second || c.timeout > 120*time.Second {
		return fmt.Errorf("WEBHOOK_TIMEOUT must be between 5s and 120s")
	}

	return nil
}