# Stage 1: Builder
FROM docker.io/library/golang:1.24-bookworm AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y gcc g++ make

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application (Dynamic linking is fine on Debian)
# We remove -tags musl and static flags
RUN CGO_ENABLED=1 GOOS=linux go build -o lgd-litestat main.go

# Stage 2: Runtime
FROM docker.io/library/debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates curl && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/lgd-litestat .
COPY --from=builder /build/database/schema_duckdb.sql ./database/
COPY --from=builder /build/database/schema_sqlite.sql ./database/

# Create data directory
RUN mkdir -p /app/data

# Expose API port
EXPOSE 8080

# Run the application
CMD ["./lgd-litestat"]
