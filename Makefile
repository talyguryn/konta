.PHONY: help build clean install run test

help:
	@echo "Konta - GitOps for Docker Compose"
	@echo ""
	@echo "Available commands:"
	@echo "  make build     - Build the binary"
	@echo "  make clean     - Clean build artifacts"
	@echo "  make install   - Build and install to /usr/local/bin"
	@echo "  make run       - Run once (requires config)"
	@echo "  make daemon    - Run daemon mode (requires config)"
	@echo "  make test      - Run tests"

build:
	@echo "Building Konta..."
	@cd cmd/konta && go build -o ../../bin/konta .
	@echo "Binary built: ./bin/konta"

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@go clean

install: build
	@echo "Installing Konta..."
	@cp bin/konta /usr/local/bin/konta
	@chmod +x /usr/local/bin/konta
	@echo "Konta installed to /usr/local/bin/konta"

run: build
	@./bin/konta run

daemon: build
	@./bin/konta daemon

test:
	@echo "Running tests..."
	@go test -v ./...

.DEFAULT_GOAL := help
