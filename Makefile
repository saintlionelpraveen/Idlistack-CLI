BINARY_NAME=idlistack
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X github.com/idlistack/cli/cmd.Version=$(VERSION)"

.PHONY: build install clean test dev

## Build the binary
build:
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) .

## Install to ~/.local/bin
install: build
	@echo "Installing to ~/.local/bin/$(BINARY_NAME)..."
	@mkdir -p $(HOME)/.local/bin
	@rm -f $(HOME)/.local/bin/$(BINARY_NAME)
	@cp bin/$(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME)
	@echo "Done! Make sure ~/.local/bin is in your PATH"

## Clean build artifacts
clean:
	@rm -rf bin/

## Run tests
test:
	go test ./... -v

## Quick development build & run
dev: build
	@./bin/$(BINARY_NAME) $(ARGS)

## Show help
help:
	@echo "IdliStack CLI - Makefile targets:"
	@echo ""
	@echo "  make build    - Build the binary"
	@echo "  make install  - Build and install to ~/.local/bin"
	@echo "  make clean    - Remove build artifacts"
	@echo "  make test     - Run tests"
	@echo "  make dev      - Quick build & run (use ARGS=... for arguments)"
