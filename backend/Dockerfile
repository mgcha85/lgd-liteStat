# Stage 1: Builder
FROM docker.io/library/golang:1.24-bookworm AS builder

# Minimal dependencies
RUN apt-get update && apt-get install -y gcc g++ make

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build without Arrow tags if possible, or just standard build and hope mod tidy cleans it
RUN CGO_ENABLED=1 GOOS=linux go build -v -o lgd-litestat main.go

FROM docker.io/library/debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates curl && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /build/lgd-litestat .
COPY --from=builder /build/database/schema_duckdb.sql ./database/
COPY --from=builder /build/database/schema_sqlite.sql ./database/

RUN mkdir -p /app/data

EXPOSE 8082

CMD ["./lgd-litestat"]
