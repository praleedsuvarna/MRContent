# Multi-stage build for optimal image size and security

# Build stage - Updated to Go 1.24 to match your local version
FROM golang:1.24-alpine AS builder

# Install git and ca-certificates (needed for private modules and HTTPS)
RUN apk add --no-cache git ca-certificates tzdata

# Create a non-root user for security
RUN adduser -D -s /bin/sh -u 1001 appuser

# Set working directory
WORKDIR /app

# Copy go mod files first (for better Docker layer caching)
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy the entire source code
COPY . .

# Build the binary with optimization flags
# CGO_ENABLED=0: Disable CGO for static binary
# -ldflags: Remove debug info and reduce binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o main .

# Production stage - use distroless for security and minimal size
FROM gcr.io/distroless/static-debian11:nonroot

# Copy timezone data for time operations
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the binary from builder stage
COPY --from=builder /app/main /app/main

# Use non-root user (already defined in distroless:nonroot)
USER nonroot:nonroot

# Expose port (Cloud Run uses PORT environment variable)
EXPOSE 8080

# Set environment variables for production
ENV APP_ENV=production
ENV GIN_MODE=release

# Health check (optional, but good practice)
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/app/main", "--health"] || exit 1

# Run the binary
ENTRYPOINT ["/app/main"]