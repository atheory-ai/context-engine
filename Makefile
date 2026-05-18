GO ?= go
GOFLAGS ?=
GORELEASER ?= goreleaser
BINARY ?= ce
BASE_VERSION := $(shell cat VERSION)
VERSION ?= $(BASE_VERSION)
PACKAGE_VERSION ?= $(VERSION)
UNIT_PACKAGES = $(shell $(GO) list ./... | grep -v '/test/acceptance$$' | grep -v '/test/coverage$$')

.PHONY: build install test test-unit test-acceptance test-coverage test-race vet fmt fmt-check verify verify-unit clean build-cross release-snapshot release-dry-run-plugins version-sync npm-stage npm-pack npm-publish help

help:
	@echo "Available targets:"
	@echo "  make build             Build ./$(BINARY)"
	@echo "  make install           Install ce with go install"
	@echo "  make test              Run all Go tests"
	@echo "  make test-unit         Run Go tests except acceptance"
	@echo "  make test-acceptance   Run CLI acceptance tests"
	@echo "  make test-coverage     Run unit coverage with package minimums"
	@echo "  make test-race         Run all Go tests with the race detector"
	@echo "  make vet               Run go vet"
	@echo "  make fmt               Format Go files"
	@echo "  make fmt-check         Fail if Go files need formatting"
	@echo "  make verify            Run fmt-check, vet, tests, and build"
	@echo "  make verify-unit       Run fmt-check, vet, unit tests, and build"
	@echo "  make build-cross       Build darwin/linux/windows for amd64/arm64"
	@echo "  make release-snapshot  Build release artifacts with GoReleaser"
	@echo "  make release-dry-run-plugins"
	@echo "                         Validate release build embeds default plugins"
	@echo "  make npm-pack          Build npm platform tarballs"
	@echo "  make clean             Remove local build artifacts"

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) ./cmd/ce

install:
	$(GO) install $(GOFLAGS) ./cmd/ce

test:
	$(GO) test $(GOFLAGS) ./...

test-unit:
	$(GO) test $(GOFLAGS) $(UNIT_PACKAGES)

test-acceptance:
	$(GO) test $(GOFLAGS) ./test/acceptance

test-coverage:
	$(GO) run ./test/coverage

test-race:
	$(GO) test $(GOFLAGS) -race ./...

vet:
	$(GO) vet $(GOFLAGS) ./...

fmt:
	@files="$$(git ls-files '*.go')"; \
	if [ -n "$$files" ]; then gofmt -w $$files; fi

fmt-check:
	@files="$$(git ls-files '*.go')"; \
	if [ -z "$$files" ]; then exit 0; fi; \
	unformatted="$$(gofmt -l $$files)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

verify: fmt-check vet test-unit test-acceptance build

verify-unit: fmt-check vet test-unit build

build-cross:
	zig version
	$(GORELEASER) build --snapshot --clean

release-snapshot:
	$(GORELEASER) build --snapshot --clean

release-dry-run-plugins:
	$(GORELEASER) --version
	GORELEASER=$(GORELEASER) ./scripts/validate-release-default-plugins.sh

version-sync:
	node scripts/set-npm-version.mjs $(PACKAGE_VERSION)

npm-stage: version-sync build-cross
	node scripts/stage-npm-binaries.mjs
	@echo "Binaries staged. Run 'make npm-pack' to create tarballs."

npm-pack: npm-stage
	cd npm/darwin-arm64 && npm pack --pack-destination ../../dist
	cd npm/darwin-x64 && npm pack --pack-destination ../../dist
	cd npm/linux-arm64 && npm pack --pack-destination ../../dist
	cd npm/linux-x64 && npm pack --pack-destination ../../dist
	cd npm/win32-arm64 && npm pack --pack-destination ../../dist
	cd npm/win32-x64 && npm pack --pack-destination ../../dist
	cd npm/ce && npm pack --pack-destination ../../dist
	@echo "Tarballs written to dist/. Inspect before publishing."

npm-publish: npm-stage
	cd npm/darwin-arm64 && npm publish --access public
	cd npm/darwin-x64 && npm publish --access public
	cd npm/linux-arm64 && npm publish --access public
	cd npm/linux-x64 && npm publish --access public
	cd npm/win32-arm64 && npm publish --access public
	cd npm/win32-x64 && npm publish --access public
	cd npm/ce && npm publish --access public
	@echo "Published @atheory-ai/ce@$(PACKAGE_VERSION) and platform packages."

clean:
	rm -f $(BINARY)
	rm -rf dist/
