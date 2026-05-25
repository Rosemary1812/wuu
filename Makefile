.PHONY: build install test vet clean release-dry snapshot print-version tag-release zig-lib browser-status browser-verify-product-defaults browser-patch-status browser-patch-check browser-patch-apply browser-launch-dev browser-verify-dev browser-import-browseros browser-import-agent

VERSION_FILE := VERSION
BASE_VERSION := $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.1.0")
VERSION ?= v$(BASE_VERSION)-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/blueberrycongee/wuu/internal/version.Version=$(VERSION) \
	-X github.com/blueberrycongee/wuu/internal/version.Commit=$(COMMIT) \
	-X github.com/blueberrycongee/wuu/internal/version.Date=$(DATE)

zig-lib:
	cd internal/jsonl/zig && zig build

build: zig-lib
	go build -tags zig -ldflags "$(LDFLAGS)" -o bin/wuu ./cmd/wuu

install: zig-lib
	go install -tags zig -ldflags "$(LDFLAGS)" ./cmd/wuu

test:
	go test ./... -count=1

test-zig: zig-lib
	go test -tags zig ./internal/jsonl/... -count=1

vet:
	go vet ./...

clean:
	rm -rf bin/ dist/

browser-status:
	bash browser/scripts/status.sh

browser-verify-product-defaults:
	bash browser/scripts/verify-product-defaults.sh

browser-patch-status:
	bash browser/scripts/patch-checkout.sh status $(ARGS)

browser-patch-check:
	bash browser/scripts/patch-checkout.sh check $(ARGS)

browser-patch-apply:
	bash browser/scripts/patch-checkout.sh apply $(ARGS)

browser-launch-dev:
	bash browser/scripts/launch-dev.sh $(ARGS)

browser-verify-dev:
	bash browser/scripts/verify-dev.sh $(ARGS)

browser-import-browseros:
	bash browser/scripts/import-browseros-assets.sh $(ARGS)

browser-import-agent:
	bash browser/scripts/import-browseros-agent.sh $(ARGS)

release-dry:
	goreleaser check
	goreleaser release --snapshot --clean --skip=publish

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
