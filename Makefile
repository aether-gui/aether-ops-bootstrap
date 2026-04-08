VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.version=$(VERSION)
BUILD_FLAGS := -trimpath -ldflags '$(LDFLAGS)'

.PHONY: build build-bundle build-all test lint install-lint vet clean

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o dist/aether-ops-bootstrap-linux-amd64 ./cmd/aether-ops-bootstrap
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -o dist/aether-ops-bootstrap-linux-arm64 ./cmd/aether-ops-bootstrap

build-bundle:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o dist/build-bundle-linux-amd64 ./cmd/build-bundle
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -o dist/build-bundle-linux-arm64 ./cmd/build-bundle

build-all: build build-bundle

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

install-lint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

vet:
	go vet ./...

clean:
	rm -rf dist/
