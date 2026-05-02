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

.PHONY: build build-minimal build-no-ui test test-integration lint \
        proto-gen proto-clean proto-lint embed-full embed-filtered release

## Build the binary with the full embedded registry (default).
build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) $(CMD)

## Build with the filtered registry (requires chains-filter.txt).
build-minimal:
	CGO_ENABLED=0 go build $(LDFLAGS) -tags registry_filtered -o $(BINARY) $(CMD)

## Build without the embedded UI bytes.
build-no-ui:
	CGO_ENABLED=0 go build $(LDFLAGS) -tags no_ui -o $(BINARY) $(CMD)

## Run all unit tests.
test:
	go test -count=1 -race ./...

## Run integration tests against a local devnet (requires a running chain).
test-integration:
	go test -count=1 -tags integration ./...

## Run golangci-lint.
lint:
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

## Fetch and embed the full chain-registry snapshot.
embed-full:
	@echo "Not yet implemented — see scripts/fetch-registry.sh (arrives in v0.2.0)"

## Fetch and embed the filtered chain-registry snapshot.
embed-filtered:
	@echo "Not yet implemented — see scripts/fetch-registry.sh (arrives in v0.2.0)"

## Build release binaries via goreleaser.
release:
	goreleaser release --clean