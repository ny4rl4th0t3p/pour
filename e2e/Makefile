## E2E test runner. Requires POUR_BIN or POUR_VERSION to be set.
##
##   make test            — run all e2e tests
##   make test-ibc        — IBC-related tests (discovery + transfers)
##   make test-auto       — auto-mode tests (devnet, hot-reload, failover)
##   POUR_BIN=../pour make test
##
## Timeout applies per suite; individual tests may take several minutes due to
## Docker container startup and relayer handshake time.

POUR_BIN  ?= ../pour

ifdef POUR_VERSION
  ENV = POUR_VERSION=$(POUR_VERSION)
else
  ENV = POUR_BIN=$(POUR_BIN)
endif

.PHONY: test test-discovery test-auto test-ibc

## Run all e2e tests.
test:
	$(ENV) go test -v -count=1 -timeout 20m ./...

## Run the IBC discovery test only.
test-discovery:
	$(ENV) go test -v -count=1 -timeout 2m -run TestIBCDiscovery ./...

## Run all auto-mode tests (devnet, self-funding, hot-reload, transport failover).
test-auto:
	$(ENV) go test -v -count=1 -timeout 5m -run TestAutoMode ./...

## Run all IBC transfer tests (happy path, native-on-dest, dual-drip, unknown denom).
test-ibc:
	$(ENV) go test -v -count=1 -timeout 15m -run TestIBCTransfer ./...