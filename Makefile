.PHONY: proto sqlc tidy test run build

MODULE := github.com/zhangjinteng/6mm-hedging-bot
ENV_FILE ?= .env
GO_TAGS ?=

proto:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	PATH="$$(go env GOPATH)/bin:$$PATH" protoc -I proto \
		--go_out=. --go_opt=module=$(MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
		proto/hedging/v1/hedging.proto

sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate

tidy:
	go mod tidy

test:
	go test ./...

run:
	@if [ -f "$(ENV_FILE)" ]; then \
		set -a; \
		. "./$(ENV_FILE)"; \
		set +a; \
	fi; \
	if [ -z "$$DATABASE_URL" ]; then \
		echo "DATABASE_URL is required. Copy .env.example to $(ENV_FILE), then set the real PostgreSQL password."; \
		exit 1; \
	fi; \
	tags="$(GO_TAGS)"; \
	if [ "$$EXCHANGE_ADAPTER" = "ccxt" ] && [ -z "$$tags" ]; then \
		tags="ccxt"; \
	fi; \
	if [ -n "$$tags" ]; then \
		go run -tags "$$tags" ./cmd/hedging-bot; \
	else \
		go run ./cmd/hedging-bot; \
	fi

build:
	@if [ -f "$(ENV_FILE)" ]; then \
		set -a; \
		. "./$(ENV_FILE)"; \
		set +a; \
	fi; \
	tags="$(GO_TAGS)"; \
	if [ "$$EXCHANGE_ADAPTER" = "ccxt" ] && [ -z "$$tags" ]; then \
		tags="ccxt"; \
	fi; \
	if [ -n "$$tags" ]; then \
		go build -tags "$$tags" -o bin/hedging-bot ./cmd/hedging-bot; \
	else \
		go build -o bin/hedging-bot ./cmd/hedging-bot; \
	fi
