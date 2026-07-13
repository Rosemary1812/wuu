.PHONY: setup dev check check-go check-desktop check-clients test test-go \
	test-desktop test-clients test-native build build-go build-desktop \
	build-clients build-macos ci install vet clean release-check release-dry \
	snapshot print-version tag-release check-release-versions

VERSION_FILE := VERSION
BASE_VERSION := $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.1.0")
VERSION ?= v$(BASE_VERSION)-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/blueberrycongee/wuu/internal/version.Version=$(VERSION) \
	-X github.com/blueberrycongee/wuu/internal/version.Commit=$(COMMIT) \
	-X github.com/blueberrycongee/wuu/internal/version.Date=$(DATE)

setup:
	npm ci --prefix desktop
	npm ci --prefix clients/core
	npm ci --prefix clients/mobile
	npm ci --prefix packages/protocol

dev:
	cd desktop && npm run dev

check: check-go check-desktop check-clients

check-go:
	go mod tidy -diff
	@test -z "$$(gofmt -l $$(find cmd internal -name '*.go' -type f))" || \
		{ echo "Go files need formatting:"; gofmt -l $$(find cmd internal -name '*.go' -type f); exit 1; }
	go vet ./...

check-desktop:
	npm --prefix desktop run typecheck

check-clients:
	npm --prefix packages/protocol run typecheck
	npm --prefix clients/core run typecheck
	npm --prefix clients/mobile run typecheck

test: test-go test-desktop test-clients

test-go:
	go test ./... -count=1

test-desktop:
	npm --prefix desktop test

test-clients:
	npm --prefix clients/core test
	npm --prefix clients/mobile test

test-native:
	npm --prefix desktop run test:cua-mac

build: build-go build-desktop build-clients

build-go:
	go build -ldflags "$(LDFLAGS)" -o bin/wuu ./cmd/wuu

build-desktop:
	npm --prefix desktop run build

build-clients: check-clients
	npm --prefix clients/mobile run export:web

build-macos:
	npm --prefix desktop run pack:mac

ci: check test build

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/wuu

vet:
	go vet ./...

clean:
	rm -rf bin/ dist/

release-dry:
	goreleaser check
	goreleaser release --snapshot --clean --skip=publish

release-check: check-release-versions
	goreleaser check
	npm pack --prefix npm --dry-run

check-release-versions:
	@core="$$(cat VERSION)"; \
	 desktop="$$(node -p "require('./desktop/package.json').version")"; \
	 npm_version="$$(node -p "require('./npm/package.json').version")"; \
	 if [ "$$core" != "$$desktop" ] || [ "$$core" != "$$npm_version" ]; then \
		echo "release versions differ: VERSION=$$core desktop=$$desktop npm=$$npm_version"; \
		exit 1; \
	 fi; \
	 echo "release versions match: $$core"

snapshot:
	goreleaser release --snapshot --clean

print-version:
	@echo v$(BASE_VERSION)

tag-release:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "working tree is dirty; commit or stash changes first"; \
		exit 1; \
	fi
	@if git rev-parse --verify --quiet "v$(BASE_VERSION)" >/dev/null; then \
		echo "tag v$(BASE_VERSION) already exists"; \
		exit 1; \
	fi
	git tag -a "v$(BASE_VERSION)" -m "release v$(BASE_VERSION)"
	@echo "created tag v$(BASE_VERSION)"
	@echo "push with: git push origin v$(BASE_VERSION)"
