GO ?= go
GOFLAGS ?=
GORELEASER ?= goreleaser
BINARY ?= ce
UNIT_PACKAGES := $(shell $(GO) list ./... | grep -v '/test/acceptance$$' | grep -v '/test/coverage$$')

.PHONY: build install test test-unit test-acceptance test-coverage test-race vet fmt fmt-check verify verify-unit clean release-snapshot help

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
	@echo "  make release-snapshot  Build release artifacts with GoReleaser"
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

release-snapshot:
	$(GORELEASER) build --snapshot --clean

clean:
	rm -f $(BINARY)
	rm -rf dist/
