.PHONY: wire dev dev-raw build test vet swagger air migrate-up migrate-down migrate-version bootstrap-admin

wire:
	cd cmd/api && wire

swagger:
	swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

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

