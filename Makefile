.PHONY: build test test-race lint install clean check cover local \
        download-ladybug-libs build-ladybug test-ladybug local-ladybug check-ladybug

GO       ?= go
GOFLAGS  ?=
BINARY   := kg
BIN_DIR  := bin
CMD_PKG  := ./cmd/kg/
GOOS     ?= $(shell $(GO) env GOOS)
GOARCH   ?= $(shell $(GO) env GOARCH)
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

# =============================================================
# LadybugDB — native library download and CGO build support
# =============================================================

LBUG_VERSION  := v0.13.1
LBUG_LIB_DIR  := lib-ladybug

# Platform-specific library filename, download asset, and rpath.
ifeq ($(GOOS),darwin)
  LBUG_LIB   := liblbug.dylib
  LBUG_ASSET := liblbug-osx-universal.tar.gz
  LBUG_RPATH := @loader_path/$(LBUG_LIB_DIR)
else
  LBUG_LIB   := liblbug.so
  ifeq ($(GOARCH),arm64)
    LBUG_ASSET := liblbug-linux-aarch64.tar.gz
  else
    LBUG_ASSET := liblbug-linux-x86_64.tar.gz
  endif
  LBUG_RPATH := $$ORIGIN/$(LBUG_LIB_DIR)
endif

# CGO environment for all ladybug targets.
# CURDIR gives the absolute repo root so the -L path is stable regardless
# of where make is invoked from.
LBUG_CGO_ENV = CGO_ENABLED=1 \
  CGO_LDFLAGS="-L$(CURDIR)/$(LBUG_LIB_DIR) -Wl,-rpath,$(LBUG_RPATH)"

# Download the native library into lib-ladybug/ (one-time; make skips if
# the file already exists).
$(LBUG_LIB_DIR)/$(LBUG_LIB):
	@echo "Downloading LadybugDB $(LBUG_VERSION) ($(LBUG_ASSET))..."
	@TMPDIR=$$(mktemp -d); \
	  curl -fsSL "https://github.com/LadybugDB/ladybug/releases/download/$(LBUG_VERSION)/$(LBUG_ASSET)" \
	    | tar xzf - -C "$$TMPDIR"; \
	  mkdir -p $(LBUG_LIB_DIR); \
	  find "$$TMPDIR" -name "$(LBUG_LIB)" -exec cp {} $(LBUG_LIB_DIR)/ \;; \
	  rm -rf "$$TMPDIR"
	@echo "  -> $(LBUG_LIB_DIR)/$(LBUG_LIB)"

download-ladybug-libs: $(LBUG_LIB_DIR)/$(LBUG_LIB) ## Download LadybugDB native library to lib-ladybug/

build-ladybug: $(LBUG_LIB_DIR)/$(LBUG_LIB) ## Build kg-ladybug (downloads native lib if needed)
	$(LBUG_CGO_ENV) $(GO) build $(GOFLAGS) -tags ladybug \
	  -ldflags "-s -w -X main.version=dev" \
	  -o $(BIN_DIR)/$(BINARY)-ladybug $(CMD_PKG)
	@mkdir -p $(BIN_DIR)/$(LBUG_LIB_DIR)
	@cp $(LBUG_LIB_DIR)/$(LBUG_LIB) $(BIN_DIR)/$(LBUG_LIB_DIR)/
	@echo "Built $(BIN_DIR)/$(BINARY)-ladybug (lib → $(BIN_DIR)/$(LBUG_LIB_DIR)/$(LBUG_LIB))"

test-ladybug: $(LBUG_LIB_DIR)/$(LBUG_LIB) ## Run tests with ladybug backend
	$(LBUG_CGO_ENV) $(GO) test -tags ladybug ./... -count=1

local-ladybug: build-ladybug ## Copy kg-ladybug + lib to .kg/bin/ for local use
	@mkdir -p .kg/bin/$(LBUG_LIB_DIR)
	cp $(BIN_DIR)/$(BINARY)-ladybug .kg/bin/kg-ladybug-$(GOOS)
	cp $(LBUG_LIB_DIR)/$(LBUG_LIB) .kg/bin/$(LBUG_LIB_DIR)/
	@echo "Installed to .kg/bin/kg-ladybug-$(GOOS)"

check-ladybug: lint test-ladybug ## Run lint then test with ladybug backend
