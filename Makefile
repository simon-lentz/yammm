# --- Grammar Generation ---

ANTLR_VERSION ?= 4.13.1
ANTLR_JAR      = antlr-$(ANTLR_VERSION)-complete.jar
ANTLR_URL      = https://www.antlr.org/download/antlr-$(ANTLR_VERSION)-complete.jar
GRAMMAR_DIR    = internal/grammar
GRAMMAR_FILE   = YammmGrammar.g4
GENERATED_DIR  = internal/grammar

.PHONY: generate-grammars
generate-grammars: $(ANTLR_JAR)
	@mkdir -p $(GENERATED_DIR)
	cd $(GRAMMAR_DIR) && $(JAVA) -jar ../$(ANTLR_JAR) -Dlanguage=Go -visitor -listener \
		-package grammar \
		-o ../$(GENERATED_DIR) \
		$(GRAMMAR_FILE)

$(ANTLR_JAR):
	curl -sSL -o $@ $(ANTLR_URL)

# --- Lint & Test ---

.PHONY: lint lint-fix
lint:
	go tool golangci-lint run

lint-fix:
	go tool golangci-lint run --fix

PUBLIC_TEST_PACKAGES := .

.PHONY: test-public test-internal
test-public:
	go test $(PUBLIC_TEST_PACKAGES)

test-internal:
	go test ./...

# --- LSP Binary ---

LSP_BINARY = yammm-lsp
LSP_CMD    = ./cmd/yammm-lsp
VERSION   ?= dev
LSP_LDFLAGS = -trimpath -ldflags="-s -w -X main.version=$(VERSION)"

# Binary output directory
LSP_BIN = bin

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
	@mkdir -p $(LSP_BIN)/$(GOOS)-$(GOARCH)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LSP_LDFLAGS) -o $(LSP_BIN)/$(GOOS)-$(GOARCH)/$(LSP_BINARY)$(BINARY_EXT) $(LSP_CMD)

# Cross-compile LSP server for all platforms
.PHONY: build-all
build-all: build-darwin-arm64 build-darwin-amd64 build-linux-amd64 build-linux-arm64 build-windows-amd64 build-windows-arm64

.PHONY: build-darwin-arm64
build-darwin-arm64:
	@mkdir -p $(LSP_BIN)/darwin-arm64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LSP_LDFLAGS) -o $(LSP_BIN)/darwin-arm64/$(LSP_BINARY) $(LSP_CMD)

.PHONY: build-darwin-amd64
build-darwin-amd64:
	@mkdir -p $(LSP_BIN)/darwin-amd64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LSP_LDFLAGS) -o $(LSP_BIN)/darwin-amd64/$(LSP_BINARY) $(LSP_CMD)

.PHONY: build-linux-amd64
build-linux-amd64:
	@mkdir -p $(LSP_BIN)/linux-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LSP_LDFLAGS) -o $(LSP_BIN)/linux-amd64/$(LSP_BINARY) $(LSP_CMD)

.PHONY: build-linux-arm64
build-linux-arm64:
	@mkdir -p $(LSP_BIN)/linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LSP_LDFLAGS) -o $(LSP_BIN)/linux-arm64/$(LSP_BINARY) $(LSP_CMD)

.PHONY: build-windows-amd64
build-windows-amd64:
	@mkdir -p $(LSP_BIN)/windows-amd64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LSP_LDFLAGS) -o $(LSP_BIN)/windows-amd64/$(LSP_BINARY).exe $(LSP_CMD)

.PHONY: build-windows-arm64
build-windows-arm64:
	@mkdir -p $(LSP_BIN)/windows-arm64
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build $(LSP_LDFLAGS) -o $(LSP_BIN)/windows-arm64/$(LSP_BINARY).exe $(LSP_CMD)

# Copy LSP binaries into VS Code extension for packaging
.PHONY: copy-to-vscode
copy-to-vscode:
	rm -rf $(VSCODE_EXT)/bin
	cp -r $(LSP_BIN) $(VSCODE_EXT)/bin

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

# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(LSP_BINARY)
	rm -rf $(LSP_BIN)
	rm -rf $(VSCODE_EXT)/bin
