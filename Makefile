# --- Lint & Test ---

.PHONY: lint lint-fix
lint:
	go tool golangci-lint run

lint-fix:
	go tool golangci-lint run --fix

.PHONY: test-internal
test-internal:
	go test ./...

# Round-trips the neo4j adapter against a real server in Docker: the emitted DDL
# is executed, the introspection queries are run for real, and what comes back is
# diffed against what was applied. Behind a build tag because it needs Docker,
# which the pre-commit gate does not.
#
# Runs the whole server matrix, one image after another. Enterprise entries start
# with the evaluation licence accepted, because that is the edition the adapter
# targets: NOT NULL, TYPE, and NODE KEY constraints and the propertyType column
# are all Enterprise-only. The Community entry exercises the edition gating,
# where those tests skip rather than fail.
#
# NEO4J_TEST_IMAGES mirrors the matrix in .github/workflows/yammm_test.yml. The
# two must move together. A green over one image is not evidence that CI passes:
# the servers differ in more than their constraint vocabulary, and a difference
# in how each recovers a connection after a rejected query reached a pull request
# once, green on the default image and red on the oldest one.
#
# To run one image only:
#
#   YAMMM_NEO4J_TEST_IMAGE=neo4j:2026.05.0-community make test-integration
ifdef YAMMM_NEO4J_TEST_IMAGE
NEO4J_TEST_IMAGES := $(YAMMM_NEO4J_TEST_IMAGE)
else
NEO4J_TEST_IMAGES ?= neo4j:5.26-enterprise neo4j:2026.05.0-enterprise neo4j:2026.05.0-community
endif

# Every image runs even after one fails, matching the workflow's fail-fast: false.
# Knowing that a change breaks the old server and not the new one is the point.
# -count=1 because the environment variable that selects the image is read at run
# time: a cached result from another image would be reported as this one's pass.
.PHONY: test-integration
test-integration:
	@failed=""; \
	for image in $(NEO4J_TEST_IMAGES); do \
		echo "==> $$image"; \
		YAMMM_NEO4J_TEST_IMAGE=$$image go test -count=1 -tags neo4j_integration \
			-timeout 20m ./adapter/neo4j/integration/ || failed="$$failed $$image"; \
	done; \
	if [ -n "$$failed" ]; then echo "integration tests failed on:$$failed"; exit 1; fi

# --- LSP Binary ---

LSP_BINARY = yammm-lsp
LSP_CMD    = ./cmd/yammm-lsp
VERSION   ?= dev
LSP_LDFLAGS = -trimpath -ldflags="-s -w -X main.version=$(VERSION)"

# Shared binary output directory (LSP + CLI cross-compile targets)
BIN_DIR = bin

# VS Code extension directory
VSCODE_EXT = lsp/editors/vscode

# Detect current platform
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
ifeq ($(GOOS),windows)
  BINARY_EXT = .exe
else
  BINARY_EXT =
endif

# Build LSP server for current platform (output to working directory)
.PHONY: build
build:
	go build $(LSP_LDFLAGS) -o $(LSP_BINARY) $(LSP_CMD)

# Build LSP server for current (native) platform into bin/
.PHONY: build-native
build-native:
	@mkdir -p $(BIN_DIR)/$(GOOS)-$(GOARCH)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LSP_LDFLAGS) -o $(BIN_DIR)/$(GOOS)-$(GOARCH)/$(LSP_BINARY)$(BINARY_EXT) $(LSP_CMD)

# Cross-compile LSP server for all platforms
.PHONY: build-all
build-all: build-darwin-arm64 build-darwin-amd64 build-linux-amd64 build-linux-arm64 build-windows-amd64 build-windows-arm64

.PHONY: build-darwin-arm64
build-darwin-arm64:
	@mkdir -p $(BIN_DIR)/darwin-arm64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LSP_LDFLAGS) -o $(BIN_DIR)/darwin-arm64/$(LSP_BINARY) $(LSP_CMD)

.PHONY: build-darwin-amd64
build-darwin-amd64:
	@mkdir -p $(BIN_DIR)/darwin-amd64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LSP_LDFLAGS) -o $(BIN_DIR)/darwin-amd64/$(LSP_BINARY) $(LSP_CMD)

.PHONY: build-linux-amd64
build-linux-amd64:
	@mkdir -p $(BIN_DIR)/linux-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LSP_LDFLAGS) -o $(BIN_DIR)/linux-amd64/$(LSP_BINARY) $(LSP_CMD)

.PHONY: build-linux-arm64
build-linux-arm64:
	@mkdir -p $(BIN_DIR)/linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LSP_LDFLAGS) -o $(BIN_DIR)/linux-arm64/$(LSP_BINARY) $(LSP_CMD)

.PHONY: build-windows-amd64
build-windows-amd64:
	@mkdir -p $(BIN_DIR)/windows-amd64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LSP_LDFLAGS) -o $(BIN_DIR)/windows-amd64/$(LSP_BINARY).exe $(LSP_CMD)

.PHONY: build-windows-arm64
build-windows-arm64:
	@mkdir -p $(BIN_DIR)/windows-arm64
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build $(LSP_LDFLAGS) -o $(BIN_DIR)/windows-arm64/$(LSP_BINARY).exe $(LSP_CMD)

# Copy LSP binaries into VS Code extension for packaging
.PHONY: copy-to-vscode
copy-to-vscode:
	rm -rf $(VSCODE_EXT)/bin
	cp -r $(BIN_DIR) $(VSCODE_EXT)/bin

# Build VS Code extension (native platform only, for development)
.PHONY: build-vscode
build-vscode: build-native copy-to-vscode
	cd $(VSCODE_EXT) && npm ci --no-audit && npm run compile

# Build VS Code extension for all platforms (for releases)
.PHONY: build-vscode-all
build-vscode-all: build-all copy-to-vscode
	cd $(VSCODE_EXT) && npm ci --no-audit && npm run compile

# Package VS Code extension
.PHONY: package-vscode
package-vscode: build-vscode
	cd $(VSCODE_EXT) && npm run package

# --- CLI Binary ---

CLI_BINARY = yammm
CLI_CMD    = ./cmd/yammm
CLI_LDFLAGS = -trimpath -ldflags="-s -w -X main.version=$(VERSION)"

# Build CLI for current platform (output to working directory)
.PHONY: build-cli
build-cli:
	go build $(CLI_LDFLAGS) -o $(CLI_BINARY) $(CLI_CMD)

# Build CLI for current (native) platform into bin/
.PHONY: build-cli-native
build-cli-native:
	@mkdir -p $(BIN_DIR)/$(GOOS)-$(GOARCH)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(CLI_LDFLAGS) -o $(BIN_DIR)/$(GOOS)-$(GOARCH)/$(CLI_BINARY)$(BINARY_EXT) $(CLI_CMD)

# Cross-compile CLI for all platforms
.PHONY: build-cli-all
build-cli-all: build-cli-darwin-arm64 build-cli-darwin-amd64 build-cli-linux-amd64 build-cli-linux-arm64 build-cli-windows-amd64 build-cli-windows-arm64

.PHONY: build-cli-darwin-arm64
build-cli-darwin-arm64:
	@mkdir -p $(BIN_DIR)/darwin-arm64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(CLI_LDFLAGS) -o $(BIN_DIR)/darwin-arm64/$(CLI_BINARY) $(CLI_CMD)

.PHONY: build-cli-darwin-amd64
build-cli-darwin-amd64:
	@mkdir -p $(BIN_DIR)/darwin-amd64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(CLI_LDFLAGS) -o $(BIN_DIR)/darwin-amd64/$(CLI_BINARY) $(CLI_CMD)

.PHONY: build-cli-linux-amd64
build-cli-linux-amd64:
	@mkdir -p $(BIN_DIR)/linux-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(CLI_LDFLAGS) -o $(BIN_DIR)/linux-amd64/$(CLI_BINARY) $(CLI_CMD)

.PHONY: build-cli-linux-arm64
build-cli-linux-arm64:
	@mkdir -p $(BIN_DIR)/linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(CLI_LDFLAGS) -o $(BIN_DIR)/linux-arm64/$(CLI_BINARY) $(CLI_CMD)

.PHONY: build-cli-windows-amd64
build-cli-windows-amd64:
	@mkdir -p $(BIN_DIR)/windows-amd64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(CLI_LDFLAGS) -o $(BIN_DIR)/windows-amd64/$(CLI_BINARY).exe $(CLI_CMD)

.PHONY: build-cli-windows-arm64
build-cli-windows-arm64:
	@mkdir -p $(BIN_DIR)/windows-arm64
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build $(CLI_LDFLAGS) -o $(BIN_DIR)/windows-arm64/$(CLI_BINARY).exe $(CLI_CMD)

# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(LSP_BINARY) $(CLI_BINARY)
	rm -rf $(BIN_DIR)
	rm -rf $(VSCODE_EXT)/bin
