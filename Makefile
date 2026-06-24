# Jenny cross-platform build targets

.PHONY: build build-all test lint

# Build the jenny binary for the current platform
build:
	go build -o jenny ./cmd/jenny/

# Build jenny binary for all three major platforms
build-all:
	./scripts/build.sh dev all

# Run all tests
test:
	go test ./...

# Run linter
lint:
	go fmt ./...
	go vet ./...
