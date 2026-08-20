# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Install ca-certificates and tzdata
RUN apk add --no-cache ca-certificates tzdata

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary with CGO disabled
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/bin/api ./cmd/api

# Final runtime stage
FROM alpine:3.20

WORKDIR /app

# Copy ca-certificates and timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary and configuration files
COPY --from=builder /build/bin/api /app/bin/api
COPY --from=builder /build/configs /app/configs
COPY --from=builder /build/migrations /app/migrations

ENV TZ=Asia/Shanghai

EXPOSE 8000

CMD ["/app/bin/api"]
