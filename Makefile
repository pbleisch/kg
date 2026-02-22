.PHONY: build test test-race lint install clean check cover local build-ladybug test-ladybug local-ladybug check-ladybug

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

build-ladybug: ## Build kg with ladybug tag
	$(GO) build $(GOFLAGS) -tags ladybug -o $(BIN_DIR)/$(BINARY)-ladybug $(CMD_PKG)

test-ladybug: ## Run tests with ladybug tag
	$(GO) test -tags ladybug ./... -count=1

local-ladybug: build-ladybug ## Copy ladybug binary to .kg/bin for local testing
	@mkdir -p .kg/bin
	cp $(BIN_DIR)/$(BINARY)-ladybug .kg/bin/kg-ladybug-$(GOOS)
	@echo "Installed to .kg/bin/kg-ladybug-$(GOOS)"

check-ladybug: lint test-ladybug ## Run lint then test with ladybug tag
