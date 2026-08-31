.PHONY: wire dev dev-raw build test vet swagger air migrate-up migrate-version bootstrap-admin proto proto-check grpc-e2e test-netconfig-integration

# 从 engine/proto/aivision/v1 权威 proto 重新生成 Go protobuf/gRPC 代码（提交目录）
proto:
	bash scripts/generate-proto.sh

# 生成到临时目录并与提交的生成文件逐文件比较，不污染工作树
proto-check:
	@tmp=$$(mktemp -d); \
	bash scripts/generate-proto.sh "$$tmp" && \
	diff -r --exclude='*_test.go' "$$tmp/internal/proto/aivision/v1" internal/proto/aivision/v1 && \
	echo "proto-check: no drift" && \
	rm -rf "$$tmp"

wire:
	cd cmd/api && wire

swagger:
	swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal

migrate-up:
	go run ./cmd/migrate up

migrate-version:
	go run ./cmd/migrate version

bootstrap-admin:
	go run ./cmd/bootstrap

dev: migrate-up
	air -c .air.toml

dev-raw: migrate-up
	go run ./cmd/api

air:
	air -c .air.toml

build: swagger
	go build -o bin/api ./cmd/api
	go build -o bin/migrate ./cmd/migrate
	go build -o bin/bootstrap ./cmd/bootstrap

test:
	go test ./...

vet:
	go vet ./...

# Linux netns 特权集成测试（需要 root/CAP_NET_ADMIN）
test-netconfig-integration:
	NETCONFIG_NETNS_TEST=1 sudo -E go test -tags=netconfig_integration -v ./internal/pkg/netconfig/...

# 跨语言 E2E：构建 mock aivision-engine 并验证 Go <-> C++ 双向 gRPC 通信（独立于普通 test）
grpc-e2e:
	bash scripts/test-grpc-engine-smoke.sh

