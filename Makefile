MODULE = github.com/ny4rl4th0t3p/pour
BINARY = pour
CMD    = ./cmd/pour

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS = -ldflags "\
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.date=$(DATE)"

.PHONY: build build-no-ui test coverage test-smoke lint \
        proto-gen proto-clean proto-lint release

## Build the binary.
build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) $(CMD)

## Build without the embedded UI bytes.
build-no-ui:
	CGO_ENABLED=0 go build $(LDFLAGS) -tags no_ui -o $(BINARY) $(CMD)

## Run all unit tests.
test:
	go test -count=1 -race ./...

## Print per-package and per-function coverage, excluding generated code.
COVERAGE_EXCLUDE = internal/tx/internal/proto|internal/ui
coverage:
	go test -count=1 -coverprofile=coverage.out -coverpkg=./... ./...
	@grep -Ev "$(COVERAGE_EXCLUDE)" coverage.out > coverage.filtered.out
	@echo "--- per-function ---"
	go tool cover -func=coverage.filtered.out | grep -v "^total"
	@echo "--- total (handwritten code only) ---"
	go tool cover -func=coverage.filtered.out | grep "^total"

## Run end-to-end smoke test against a Docker Compose devnet (requires Docker).
test-smoke:
	@docker compose -f tests/smoke/docker-compose.yml up \
	    --build --abort-on-container-exit --exit-code-from smoke ; \
	  STATUS=$$? ; docker compose -f tests/smoke/docker-compose.yml down -v ; exit $$STATUS

## Run golangci-lint.
lint:
	golangci-lint cache clean
	golangci-lint run

## Generate Go bindings from vendored .proto files.
proto-gen:
	cd proto && buf generate

## Remove generated proto bindings.
proto-clean:
	rm -rf internal/tx/internal/proto/

## Lint vendored .proto files.
proto-lint:
	cd proto && buf lint

## Build release binaries via goreleaser.
release:
	goreleaser release --clean