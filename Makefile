# Makefile for the CatWatch app
# Usage: `make <target>`

SHELL := /bin/bash
GO     ?= go
BINARY ?= catwatch
BOT_BINARY ?= catwatch_bot
CMD     = ./cmd/catwatch
BOT_CMD = ./cmd/catwatch_bot
BIN_DIR = bin
VERSION ?= $(shell git describe --tags --long --always 2>/dev/null || echo "v0.0.0-dev")
BUILDTIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ || echo "1970-01-01T00:00:00Z")
LDFLAGS ?= -s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILDTIME)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help message
	@echo "Available targets:" && echo && \
	awk 'BEGIN {FS = ":.*?## "}; /^[a-zA-Z0-9_\-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

.PHONY: build
build: $(BIN_DIR)/$(BINARY) $(BIN_DIR)/$(BOT_BINARY) ## Build binaries in ./bin/

$(BIN_DIR)/$(BINARY): tidy vendor fmt vet test generate ## Build main application
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)
	@echo "Built $(BIN_DIR)/$(BINARY) $(VERSION) $(BUILDTIME)"

$(BIN_DIR)/$(BOT_BINARY): tidy vendor fmt vet test generate ## Build Telegram bot
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BOT_BINARY) $(BOT_CMD)
	@echo "Built $(BIN_DIR)/$(BOT_BINARY) $(VERSION) $(BUILDTIME)"

.PHONY: run
run: tidy vendor fmt vet test generate ## Run application directly via go run (use env vars and ARGS="--flag=value")
	DB_PATH="./catwatch.db" $(GO) run $(CMD) $(ARGS)

.PHONY: dev
dev: tidy vendor fmt vet test generate ## Run in dev mode (DEBUG=1)
	DEBUG=1 DEV_LOGIN=1 DB_PATH="./catwatch.db" $(GO) run $(CMD) $(ARGS)

.PHONY: tidy
tidy: ## Update dependencies (go mod tidy)
	$(GO) mod tidy

.PHONY: vendor
vendor: ## Vendor dependencies (go mod vendor)
	$(GO) mod vendor

.PHONY: fmt
fmt: ## Format code (go fmt)
	$(GO) fmt ./...

.PHONY: vet
vet: ## Static analysis (go vet)
	$(GO) vet ./...

.PHONY: test
test: ## Run tests
	$(GO) test ./...

.PHONY: clean
clean: ## Clean build artifacts
	rm -f $(BIN_DIR)/$(BINARY)
	rm -f $(BIN_DIR)/$(BOT_BINARY)
	rm -f ./catwatch.db
	rm -f internal/frontend/static/js/app.bundle.js
	rm -f internal/frontend/static/js/app.bundle.js.map

.PHONY: generate frontend
generate: ## Generate frontend (go generate -> esbuild)
	@command -v esbuild >/dev/null || (echo "esbuild not found. Install: 'brew install esbuild' or 'npm i -g esbuild'"; exit 1)
	$(GO) generate ./...

frontend: generate ## Alias for generate
