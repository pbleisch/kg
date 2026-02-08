.PHONY: build test test-race lint install clean check cover local

GO       ?= go
GOFLAGS  ?=
BINARY   := kg
BIN_DIR  := bin
CMD_PKG  := ./cmd/kg/
GOOS     ?= $(shell $(GO) env GOOS)
LOCAL_BIN := .kg/bin/kg-$(GOOS)

build: ## Build the kg binary
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD_PKG)

local: build ## Copy built binary to .kg/bin/kg-<os> for local testing
	@mkdir -p .kg/bin
	cp $(BIN_DIR)/$(BINARY) $(LOCAL_BIN)
	@echo "Installed to $(LOCAL_BIN)"

test: ## Run tests
	$(GO) test ./... -count=1

test-race: ## Run tests with race detector
	$(GO) test -race ./... -count=1

lint: ## Run go vet
	$(GO) vet ./...

install: ## Install kg to GOPATH/bin
	$(GO) install $(GOFLAGS) $(CMD_PKG)

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

check: lint test ## Run lint then test (CI convenience)

cover: ## Run tests with coverage and generate coverage.html
	$(GO) test ./... -count=1 -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html
