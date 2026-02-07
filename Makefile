.PHONY: build test test-race lint install clean check cover

GO       ?= go
GOFLAGS  ?=
BINARY   := kg
BIN_DIR  := bin
CMD_PKG  := ./cmd/kg/

build: ## Build the kg binary
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD_PKG)

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
