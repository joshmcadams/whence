BINARY  := whence
PKG     := ./cmd/whence
BIN_DIR := bin

# Version is injected into the binary (same path GoReleaser uses).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/joshmcadams/whence/internal/cli.version=$(VERSION)

# Args passed to `make run`, e.g. `make run ARGS="list --all"`.
ARGS ?=

.DEFAULT_GOAL := build

.PHONY: build
build: ## Compile the binary to bin/whence
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

.PHONY: run
run: ## Run without installing, e.g. `make run ARGS="list --all"` or `make run ARGS=tui`
	go run -ldflags "$(LDFLAGS)" $(PKG) $(ARGS)

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: lint
lint: ## Check formatting and run go vet (plus golangci-lint if installed)
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needs to run on:"; echo "$$unformatted"; exit 1; \
	fi
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed; ran gofmt + go vet only"; \
	fi

.PHONY: fmt
fmt: ## Format the code in place
	gofmt -w .

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN_DIR)

.PHONY: help
help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
