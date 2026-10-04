.PHONY: build build-linux build-windows build-all clean install

VERSION_PACKAGE := github.com/valentin-kaiser/go-core/version
GIT_TAG := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
GIT_SHORT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date +%FT%T%z 2>/dev/null || echo unknown)

LDFLAGS := -X $(VERSION_PACKAGE).GitTag=$(GIT_TAG) \
	-X $(VERSION_PACKAGE).GitCommit=$(GIT_COMMIT) \
	-X $(VERSION_PACKAGE).GitShort=$(GIT_SHORT) \
	-X $(VERSION_PACKAGE).BuildDate=$(BUILD_TIME)

# Build for current platform
build:
	go build -ldflags "$(LDFLAGS)" -o protoc-gen-go-xrpc$(shell go env GOEXE) ./cmd/protoc-gen-go-xrpc

# Build for Linux
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o protoc-gen-go-xrpc ./cmd/protoc-gen-go-xrpc

# Build for Windows
build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o protoc-gen-go-xrpc.exe ./cmd/protoc-gen-go-xrpc

# Build for multiple platforms
build-all: build-linux build-windows

# Clean build artifacts
clean:
	rm -f protoc-gen-go-xrpc protoc-gen-go-xrpc.exe

# Install to GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/protoc-gen-go-xrpc
