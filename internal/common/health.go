package common

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// HealthServer provides a simple HTTP health check endpoint
type HealthServer struct {
	server *http.Server
	logger *logrus.Entry
	port   int
}

// NewHealthServer creates a new health check HTTP server
func NewHealthServer() *HealthServer {
	port := getEnvAsInt("HEALTH_CHECK_PORT", 8080)

	logger := logrus.WithFields(logrus.Fields{
		"component": "health_server",
		"port":      port,
	})

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

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	return &HealthServer{
		server: server,
		logger: logger,
		port:   port,
	}
}

// Start starts the health check server in a goroutine
func (h *HealthServer) Start() {
	go func() {
		h.logger.WithField("port", h.port).Info("Health check server starting")
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.logger.WithError(err).Error("Health check server failed")
		}
	}()
}

// Stop gracefully shuts down the health check server
func (h *HealthServer) Stop(ctx context.Context) error {
	h.logger.Info("Shutting down health check server")
	return h.server.Shutdown(ctx)
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
