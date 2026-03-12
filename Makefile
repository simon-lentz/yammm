ANTLR_VERSION ?= 4.13.1
ANTLR_JAR      = antlr-$(ANTLR_VERSION)-complete.jar
ANTLR_URL      = https://www.antlr.org/download/antlr-$(ANTLR_VERSION)-complete.jar
GRAMMAR_DIR    = grammar
GRAMMAR_FILE   = YammmGrammar.g4
GENERATED_DIR  = grammar

.PHONY: generate-grammars
generate-grammars: $(ANTLR_JAR)
	@mkdir -p $(GENERATED_DIR)
	cd $(GRAMMAR_DIR) && $(JAVA) -jar ../$(ANTLR_JAR) -Dlanguage=Go -visitor -listener \
		-package grammar \
		-o ../$(GENERATED_DIR) \
		$(GRAMMAR_FILE)

$(ANTLR_JAR):
	curl -sSL -o $@ $(ANTLR_URL)

.PHONY: lint lint-fix

lint:
	go tool golangci-lint run

lint-fix:
	go tool golangci-lint run --fix

PUBLIC_TEST_PACKAGES := .

.PHONY: test-public
test-public:
	go test $(PUBLIC_TEST_PACKAGES)

.PHONY: test-internal
test-internal:
	go test ./...
