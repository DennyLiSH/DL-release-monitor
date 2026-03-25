# gh-release-monitor Makefile

.PHONY: build test lint run clean help

# Binary name
BINARY_NAME=gh-release-monitor
BINARY_PATH=bin/$(BINARY_NAME)

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Main package
MAIN_PACKAGE=./cmd/gh-release-monitor

## build: Build the binary
build:
	$(GOBUILD) -o $(BINARY_PATH) $(MAIN_PACKAGE)

## test: Run all tests
test:
	$(GOTEST) -v ./...

## test-race: Run tests with race detection
test-race:
	$(GOTEST) -race -v ./...

## coverage: Run tests with coverage
coverage:
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

## lint: Run golangci-lint
lint:
	golangci-lint run

## vet: Run go vet
vet:
	$(GOCMD) vet ./...

## fmt: Format code
fmt:
	gofmt -s -w .

## tidy: Tidy go modules
tidy:
	$(GOMOD) tidy

## run: Run the application
run:
	$(GOCMD) run $(MAIN_PACKAGE)

## clean: Clean build artifacts
clean:
	$(GOCLEAN)
	rm -rf bin/
	rm -f coverage.out coverage.html

## check: Run all checks (fmt, vet, test)
check: fmt vet test

## help: Show this help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':'
