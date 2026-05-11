GO ?= go
GOFLAGS ?=
GORELEASER ?= goreleaser
BINARY ?= ce

.PHONY: build install test test-race vet fmt fmt-check verify clean release-snapshot help

help:
	@echo "Available targets:"
	@echo "  make build             Build ./$(BINARY)"
	@echo "  make install           Install ce with go install"
	@echo "  make test              Run all Go tests"
	@echo "  make test-race         Run all Go tests with the race detector"
	@echo "  make vet               Run go vet"
	@echo "  make fmt               Format Go files"
	@echo "  make fmt-check         Fail if Go files need formatting"
	@echo "  make verify            Run fmt-check, vet, tests, and build"
	@echo "  make release-snapshot  Build release artifacts with GoReleaser"
	@echo "  make clean             Remove local build artifacts"

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) ./cmd/ce

install:
	$(GO) install $(GOFLAGS) ./cmd/ce

test:
	$(GO) test $(GOFLAGS) ./...

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

verify: fmt-check vet test build

release-snapshot:
	$(GORELEASER) build --snapshot --clean

clean:
	rm -f $(BINARY)
	rm -rf dist/
