VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X main.version=$(VERSION)

.PHONY: dist build build-bundle build-release-site build-all bundle package release-site test lint install-lint vet clean
.PHONY: test-e2e test-e2e-quick test-e2e-bootstrap test-e2e-deploy
.PHONY: test-e2e-multi-bootstrap test-e2e-multi-deploy
.PHONY: docs docs-build docs-serve

dist:
	mkdir -p dist

build: dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aether-ops-bootstrap ./cmd/aether-ops-bootstrap

build-bundle: dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '-X main.gitSHA=$(COMMIT)' -o dist/build-bundle ./cmd/build-bundle

build-release-site: dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/build-release-site ./cmd/build-release-site

build-all: build build-bundle

# Build the offline bundle from specs/bundle.yaml using the build-bundle tool.
bundle: build-bundle
	./dist/build-bundle --spec specs/bundle.yaml --output dist/bundle.tar.zst

# Package the bootstrap binary, bundle, and hash into a single distributable archive.
package: build bundle
	cd dist && tar czf aether-ops-bootstrap-$(VERSION).tar.gz \
		aether-ops-bootstrap \
		bundle.tar.zst \
		bundle.tar.zst.sha256

release-site: build-release-site
	./dist/build-release-site --metadata site/releases.example.yaml --output dist/release-site

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

install-lint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

vet:
	go vet ./...

# E2E tests (require DART CLI and LXD with a 'default' storage pool)
# Each suite's setup/00_build-artifacts.yaml rebuilds the binary and bundle
# on the host before pushing to the VM, so no build prereq is needed here.
# Quick tests verify bootstrap only (~10-15 min each)
test-e2e-bootstrap:
	dart -c tests/single-node-bootstrap/single-node-bootstrap.yaml -s

test-e2e-multi-bootstrap:
	dart -c tests/multi-node-bootstrap/multi-node-bootstrap.yaml -s

# Full tests deploy SD-Core via the aether-ops API (~30-45 min each)
test-e2e-deploy:
	dart -c tests/single-node-deploy/single-node-deploy.yaml -s

test-e2e-multi-deploy:
	dart -c tests/multi-node-deploy/multi-node-deploy.yaml -s

# Convenience targets
test-e2e-quick: test-e2e-bootstrap test-e2e-multi-bootstrap
test-e2e: test-e2e-quick test-e2e-deploy test-e2e-multi-deploy

clean:
	rm -rf dist/

# Docusaurus docs site (published to GitHub Pages via .github/workflows/docs.yml)
docs:
	cd website && npm start

docs-build:
	cd website && npm ci && npm run build

docs-serve: docs-build
	cd website && npm run serve
