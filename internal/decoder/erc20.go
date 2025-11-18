package decoder

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
	"indexer-go-v2/pkg/types"
)

// ERC20ABI represents the ERC20 token ABI
var ERC20ABI, _ = abi.JSON(strings.NewReader(`[
	{
		"anonymous": false,
		"inputs": [
			{"indexed": true, "name": "from", "type": "address"},
			{"indexed": true, "name": "to", "type": "address"},
			{"indexed": false, "name": "value", "type": "uint256"}
		],
		"name": "Transfer",
		"type": "event"
	}
]`))

// Decoder handles ERC20 event decoding
type Decoder struct {
	logger *logrus.Entry
}

// NewDecoder creates a new ERC20 decoder
func NewDecoder() *Decoder {
	return &Decoder{
		logger: logrus.WithField("component", "erc20_decoder"),
	}
}

// DecodeTransferEvent decodes an ERC20 Transfer event log
func (d *Decoder) DecodeTransferEvent(log *types.ERC20Log, currencies []*types.Currency, chainID int) (*types.PaymentEvent, error) {
	// Verify this is a Transfer event
	if len(log.Topics) < 1 || log.Topics[0] != types.TransferEventSignature {
		return nil, fmt.Errorf("not a Transfer event")
	}

	// Find the currency that matches the log address
	var currency *types.Currency
	for _, curr := range currencies {
		if strings.EqualFold(curr.ContractAddress, log.Address) {
			currency = curr
			break
		}
	}

	if currency == nil {
		return nil, fmt.Errorf("currency not found for contract address %s", log.Address)
	}

	// Decode the log data (contains the amount)
	amount, ok := new(big.Int).SetString(log.Data[2:], 16)
	if !ok {
		return nil, fmt.Errorf("failed to parse amount from log data")
	}

	// Extract addresses from topics
	fromAddress := common.HexToAddress(log.Topics[1])
	toAddress := common.HexToAddress(log.Topics[2])

	// Parse block number
	blockNumber, err := strconv.ParseInt(strings.TrimPrefix(log.BlockNumber, "0x"), 16, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse block number: %w", err)
	}

	// Parse log index
	logIndex, err := strconv.ParseInt(strings.TrimPrefix(log.LogIndex, "0x"), 16, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse log index: %w", err)
	}

	// Create event ID using the provided chainID parameter (from worker config)
	eventID := types.GenerateEventID(chainID, log.TransactionHash, int(logIndex))

	// Format amount for human readability
	amountFormatted := d.formatAmount(amount, currency.Decimals)

	// Create payment event
	paymentEvent := &types.PaymentEvent{
		ID:              eventID,
		ChainID:         chainID, // Use the converted chainID from above
		TransactionHash: log.TransactionHash,
		LogIndex:        int(logIndex),
		BlockNumber:     blockNumber,
		BlockHash:       log.BlockHash,
		FromAddress:     fromAddress.Hex(),
		ToAddress:       toAddress.Hex(),
		TokenAddress:    currency.ContractAddress,
		TokenSymbol:     currency.Symbol,
		TokenName:       currency.Name,
		TokenDecimals:   currency.Decimals,
		Amount:          amount.String(),
		AmountFormatted: amountFormatted,
		Timestamp:       d.parseTimestamp(log),
		Status:          types.StatusDetected,
		Confirmations:   0, // Will be updated when checking confirmations
	}

	d.logger.WithFields(logrus.Fields{
		"event_id":       paymentEvent.ID,
		"tx_hash":        paymentEvent.TransactionHash,
		"token_symbol":   paymentEvent.TokenSymbol,
		"from":           paymentEvent.FromAddress,
		"to":             paymentEvent.ToAddress,
		"amount":         paymentEvent.AmountFormatted,
		"block_number":   paymentEvent.BlockNumber,
	}).Debug("Decoded ERC20 Transfer event")

	return paymentEvent, nil
}

// IsWatchedAddress checks if an address is in the watched list
func (d *Decoder) IsWatchedAddress(address string, watchedAddresses []string) bool {
	for _, watched := range watchedAddresses {
		if strings.EqualFold(address, watched) {
			return true
		}
	}
	return false
}

// DecodeBatchLogs decodes multiple logs in parallel
func (d *Decoder) DecodeBatchLogs(logs []*types.ERC20Log, currencies []*types.Currency, watchedAddresses []string, chainID int) ([]*types.PaymentEvent, error) {
	events := make([]*types.PaymentEvent, 0, len(logs))

	for _, log := range logs {
		// Check if this is a Transfer event
		if len(log.Topics) == 0 || log.Topics[0] != types.TransferEventSignature {
			continue
		}

		// Extract to address from topics
		if len(log.Topics) < 3 {
			continue
		}

		toAddress := common.HexToAddress(log.Topics[2]).Hex()

		// Check if this is to a watched address
		if !d.IsWatchedAddress(toAddress, watchedAddresses) {
			continue
		}

		// Decode the event
		event, err := d.DecodeTransferEvent(log, currencies, chainID)
		if err != nil {
			d.logger.WithFields(logrus.Fields{
				"tx_hash": log.TransactionHash,
				"error":   err.Error(),
			}).Warn("Failed to decode Transfer event")
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// ParseLogTopics converts log topics to proper format
func (d *Decoder) ParseLogTopics(topics []string) ([][]string, error) {
	if len(topics) == 0 {
		return nil, fmt.Errorf("no topics provided")
	}

	// Convert Transfer event signature to hex format
	transferSig := types.TransferEventSignature

	var result [][]string
	result = append(result, []string{transferSig})

	// Add other topics (from, to addresses)
	for i := 1; i < len(topics); i++ {
		if topics[i] != "" {
			result = append(result, []string{topics[i]})
		} else {
			result = append(result, nil)
		}
	}

	return result, nil
}

// BuildAddressTopics creates topics for filtering by addresses
func (d *Decoder) BuildAddressTopics(watchedAddresses []string) []string {
	var topics []string

	for _, address := range watchedAddresses {
		if address == "" {
			continue
		}

		// Convert address to topic format (32 bytes padded)
		if !strings.HasPrefix(address, "0x") {
			address = "0x" + address
		}

		// Normalize address and create topic
		addr := common.HexToAddress(address)
		topic := common.BytesToHash(addr.Bytes()).Hex()
		topics = append(topics, topic)
	}

	return topics
}

// ValidateTransferLog validates that a log is a valid Transfer event
func (d *Decoder) ValidateTransferLog(log *types.ERC20Log) error {
	// Check if log has required fields
	if log.Address == "" {
		return fmt.Errorf("missing contract address")
	}

	if log.Topics == nil || len(log.Topics) < 3 {
		return fmt.Errorf("insufficient topics for Transfer event")
	}

	if log.Topics[0] != types.TransferEventSignature {
		return fmt.Errorf("not a Transfer event signature")
	}

	if log.Data == "" {
		return fmt.Errorf("missing log data")
	}

	if log.TransactionHash == "" {
		return fmt.Errorf("missing transaction hash")
	}

	if log.BlockNumber == "" {
		return fmt.Errorf("missing block number")
	}

	return nil
}

// GetTokenForAddress finds token configuration by contract address
func (d *Decoder) GetTokenForAddress(address string, currencies []*types.Currency) (*types.Currency, error) {
	for _, currency := range currencies {
		if strings.EqualFold(currency.ContractAddress, address) {
			return currency, nil
		}
	}
	return nil, fmt.Errorf("token not found for address %s", address)
}

// Helper functions

func (d *Decoder) formatAmount(amount *big.Int, decimals int) string {
	if decimals <= 0 {
		return amount.String()
	}

	// Convert to decimal representation
	divisor := big.NewInt(10)
	divisor.Exp(divisor, big.NewInt(int64(decimals)), nil)

	// Perform division
	result := new(big.Int).Div(amount, divisor)

	return result.String()
}

func (d *Decoder) parseTimestamp(log *types.ERC20Log) time.Time {
	// In a real implementation, you might get timestamp from block data
	// For now, use current time as placeholder
	return time.Now()
}

// Helper function for string reader
func newStringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}