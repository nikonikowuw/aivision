.PHONY: wire dev build test vet

wire:
	cd cmd/api && wire

dev:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api

test:
	go test ./...

vet:
	go vet ./...
