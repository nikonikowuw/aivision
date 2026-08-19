.PHONY: wire dev build test vet swagger air

wire:
	cd cmd/api && wire

swagger:
	swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal

dev:
	air -c .air.toml

dev-raw:
	go run ./cmd/api

air:
	air -c .air.toml

build: swagger
	go build -o bin/api ./cmd/api

test:
	go test ./...

vet:
	go vet ./...

