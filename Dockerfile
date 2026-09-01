# Stage 1: Build Frontend UI
FROM node:20-alpine AS ui-builder

WORKDIR /build/ui

RUN corepack enable && corepack prepare pnpm@latest --activate

# Copy frontend source files
COPY ui/package.json ui/pnpm-lock.yaml ui/pnpm-workspace.yaml ui/turbo.json ./
COPY ui/internal ./internal
COPY ui/packages ./packages
COPY ui/apps/web-antd ./apps/web-antd

RUN pnpm install --frozen-lockfile
RUN pnpm --filter @vben/web-antd build

# Stage 2: Build Backend Go Single Binary
FROM golang:1.26-alpine AS backend-builder

WORKDIR /build/argus

RUN apk add --no-cache ca-certificates tzdata

COPY argus/go.mod argus/go.sum ./
RUN go mod download

COPY argus/ .

# Copy UI build output into Go internal embed dist directory
COPY --from=ui-builder /build/ui/apps/web-antd/dist/ ./internal/web/dist/

# Build binary with CGO disabled
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/bin/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/bin/migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/bin/bootstrap ./cmd/bootstrap

# Stage 3: Minimal Runtime
FROM alpine:3.20

WORKDIR /app

# Copy ca-certificates and timezone data
COPY --from=backend-builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=backend-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary and configuration files
COPY --from=backend-builder /build/bin/api /app/bin/api
COPY --from=backend-builder /build/bin/migrate /app/bin/migrate
COPY --from=backend-builder /build/bin/bootstrap /app/bin/bootstrap
COPY --from=backend-builder /build/argus/configs /app/configs

ENV TZ=Asia/Shanghai

EXPOSE 8000

CMD ["/app/bin/api"]
