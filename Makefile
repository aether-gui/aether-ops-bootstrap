VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X main.version=$(VERSION)

.PHONY: dist build build-bundle build-all bundle package test lint install-lint vet clean

dist:
	mkdir -p dist

build: dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aether-ops-bootstrap ./cmd/aether-ops-bootstrap

build-bundle: dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '-X main.gitSHA=$(COMMIT)' -o dist/build-bundle ./cmd/build-bundle

build-all: build build-bundle

# Build the offline bundle from bundle.yaml using the build-bundle tool.
bundle: build-bundle
	./dist/build-bundle --spec bundle.yaml --output dist/bundle.tar.zst

# Package the bootstrap binary, bundle, and hash into a single distributable archive.
package: build bundle
	cd dist && tar czf aether-ops-bootstrap-$(VERSION).tar.gz \
		aether-ops-bootstrap \
		bundle.tar.zst \
		bundle.tar.zst.sha256

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
