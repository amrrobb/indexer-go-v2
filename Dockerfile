# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the applications
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o forward ./cmd/forward
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o backfill ./cmd/backfill
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o confirmation ./cmd/confirmation

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy the binary files from builder stage
COPY --from=builder /app/forward .
COPY --from=builder /app/backfill .
COPY --from=builder /app/confirmation .

# Copy environment file
COPY .env.example .env

# Expose health check port (if needed)
EXPOSE 8080

# Run command
CMD ["./forward"]