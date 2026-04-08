VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.version=$(VERSION)

.PHONY: dist build build-bundle build-all test lint install-lint vet clean

dist:
	mkdir -p dist

build: dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aether-ops-bootstrap ./cmd/aether-ops-bootstrap

build-bundle: dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/build-bundle ./cmd/build-bundle

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
