package types

import (
	"fmt"
	"time"
)

// Chain represents a blockchain network from backend database
type Chain struct {
	ID                     string `db:"id" json:"id"`                           // UUID network ID
	NetworkID              int    `db:"chain_id" json:"chain_id"`               // EVM chain ID (42161, 56, etc)
	Network                string `db:"network" json:"network"`                 // ethereum, polygon, arbitrum, bsc
	BlockchainNetworkType  string `db:"blockchain_network_type" json:"blockchain_network_type"` // EVM
	IsActive               bool   `db:"is_active" json:"is_active"`
}

// Currency represents a token from backend database
type Currency struct {
	ID              string  `db:"id" json:"id"`
	NetworkID       string  `db:"network_id" json:"network_id"`
	ContractAddress string  `db:"contract_address" json:"contract_address"`
	Symbol          string  `db:"symbol" json:"symbol"`
	Name            string  `db:"name" json:"name"`
	Decimals        int     `db:"decimals" json:"decimals"`
	IsNative        bool    `db:"is_native" json:"is_native"`
	IsActive        bool    `db:"is_active" json:"is_active"`
	IsUsedForPayment bool   `db:"is_used_for_payment" json:"is_used_for_payment"`
}

// Wallet represents a watched address from backend database
type Wallet struct {
	ID        string `db:"id" json:"id"`
	Address   string `db:"address" json:"address"`
	NetworkID string `db:"network_id" json:"network_id"`
	Type      string `db:"type" json:"type"` // DEPOSIT for payments
	IsActive  bool   `db:"is_active" json:"is_active"`
}

// PaymentEvent represents a detected ERC20 transfer
type PaymentEvent struct {
	ID              string    `json:"id"`              // <chainId>:<txHash>:<logIndex>
	ChainID         int       `json:"chainId"`
	TransactionHash string    `json:"transactionHash"`
	LogIndex        int       `json:"logIndex"`
	BlockNumber     int64     `json:"blockNumber"`
	BlockHash       string    `json:"blockHash"`
	FromAddress     string    `json:"fromAddress"`
	ToAddress       string    `json:"toAddress"`
	TokenAddress    string    `json:"tokenAddress"`
	TokenSymbol     string    `json:"tokenSymbol"`
	TokenName       string    `json:"tokenName"`
	TokenDecimals   int       `json:"tokenDecimals"`
	Amount          string    `json:"amount"`          // Raw amount as string
	AmountFormatted string    `json:"amountFormatted"` // Human-readable format
	Timestamp       time.Time `json:"timestamp"`
	Status          string    `json:"status"`          // detected, confirmed
	Confirmations   int       `json:"confirmations"`
	FirstDetectedAt *time.Time `json:"firstDetectedAt,omitempty"`
}

// WebhookPayload represents the payload sent to backend webhook
type WebhookPayload struct {
	TxHash              string     `json:"tx_hash"`
	FromAddress         string     `json:"from_address"`
	ToAddress           string     `json:"to_address"`       // Our watched wallet
	Amount              string     `json:"amount"`           // Raw amount
	AmountFormatted     string     `json:"amount_formatted"`
	TokenAddress        string     `json:"token_address"`   // From currencies.contract_address
	Chain               string     `json:"chain"`           // ETHEREUM, POLYGON, etc.
	ChainID             int        `json:"chain_id"`        // Chain ID (1, 137, 42161, 56)
	BlockNumber         uint64     `json:"block_number"`
	Status              string     `json:"status"`          // "detected" | "confirmed"
	Confirmations       int        `json:"confirmations"`
	Timestamp           time.Time  `json:"timestamp"`
	FirstDetectedAt     *time.Time `json:"first_detected_at,omitempty"` // For confirmed
	TokenSymbol         string     `json:"token_symbol"`
	TokenName           string     `json:"name"`
	CurrentConfirmations int      `json:"current_confirmations"`
	TargetConfirmations  int      `json:"target_confirmations"`
}

// IndexerCursor represents worker position tracking
type IndexerCursor struct {
	ID         int       `db:"id" json:"id"`
	ChainID    int       `db:"chain_id" json:"chain_id"`
	WorkerType string    `db:"worker_type" json:"worker_type"` // forward | backfill
	LastBlock  int64     `db:"last_block" json:"last_block"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

// WorkerConfig represents configuration for a chain worker
type WorkerConfig struct {
	ChainID                int
	Network                string
	ChainType              string
	ConfirmationsRequired  int
	TargetConfirmations    int
	ActiveCurrencies       []Currency
	WatchedAddresses       []string
	RPCURL                 string
}

// WorkerStatus represents status information for a worker
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

// RPCRequest represents a raw JSON-RPC request
type RPCRequest struct {
	ID      string      `json:"id"`
	JsonRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// RPCResponse represents a raw JSON-RPC response
type RPCResponse struct {
	ID    string      `json:"id"`
	Error *RPCError   `json:"error,omitempty"`
	Result interface{} `json:"result,omitempty"`
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ERC20Log represents a decoded ERC20 Transfer event log
type ERC20Log struct {
	Address          string   `json:"address"`
	BlockHash        string   `json:"blockHash"`
	BlockNumber      string   `json:"blockNumber"`
	LogIndex         string   `json:"logIndex"`
	Removed          bool     `json:"removed"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex string   `json:"transactionIndex"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
}

// TransferEvent represents a decoded ERC20 Transfer event
type TransferEvent struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Value string `json:"value"`
}

// Constants for event signatures and status
const (
	TransferEventSignature = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	StatusDetected         = "detected"
	StatusConfirmed        = "confirmed"
	WorkerTypeForward      = "forward"
	WorkerTypeBackfill     = "backfill"
	WorkerTypeConfirmation = "confirmation"
	ChainTypeEVM           = "EVM"
	WalletTypeMerchant     = "MERCHANT_WALLET"
)

// Chain-specific confirmation requirements
var ChainConfirmations = map[string]int{
	"ethereum": 12,  // ~3 minutes
	"polygon":  20,  // ~1 minute
	"arbitrum": 10,  // ~10 seconds
	"bsc":      15,  // ~45 seconds
}

// RPC Batching Configuration
var (
	DefaultMaxAddressesPerCall = 100  // Optimized for paid Alchemy via ERPC
	DefaultMaxConcurrentCalls  = 5    // ERPC handles concurrency well
	MaxAddressesPerCall       = 1000  // Upper limit for most providers
	MinAddressesPerCall       = 10    // Minimum efficient batch size
)

// Chain names for webhook compatibility
var ChainNames = map[int]string{
	1:      "ETHEREUM",
	137:    "POLYGON",
	42161:  "ARBITRUM",
	56:     "BSC",
}

// Reverse chain names map
var ChainIDs = map[string]int{
	"ETHEREUM": 1,
	"POLYGON":  137,
	"ARBITRUM": 42161,
	"BSC":      56,
}

// Helper functions
func GenerateEventID(chainID int, txHash string, logIndex int) string {
	return fmt.Sprintf("%d:%s:%d", chainID, txHash, logIndex)
}

func GetChainName(chainID int) string {
	if name, exists := ChainNames[chainID]; exists {
		return name
	}
	return "UNKNOWN"
}

func GetChainID(chainName string) int {
	if id, exists := ChainIDs[chainName]; exists {
		return id
	}
	return 0
}

// Block represents a blockchain block (simplified version)
type Block struct {
	Number     string   `json:"number"`
	Hash       string   `json:"hash"`
	ParentHash string   `json:"parentHash"`
	Timestamp  string   `json:"timestamp"`
}

// Receipt represents a transaction receipt (simplified version)
type Receipt struct {
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     string   `json:"blockNumber"`
	Status          string   `json:"status"`
	Logs            []ERC20Log `json:"logs"`
}