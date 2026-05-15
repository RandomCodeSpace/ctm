# ctm Makefile — Go build and regression orchestration.

.PHONY: build help regression

help:
	@echo "Targets:"
	@echo "  build      — go build ./..."
	@echo "  regression — full pre-merge pack: go build/test/race/vuln"

build:
	@echo "==> go build"
	go build -trimpath ./...

# Unified regression pack. Runs everything a PR must clear before merge.
# Fails fast — first non-zero exit stops the run.
#
# Contract: every shipped bug fix or new feature adds a test case that
# executes under one of these steps. The pack grows; it does not get
# replaced.
regression:
	@echo "==> go build ./..."
	go build ./...
	@echo "==> go test ./..."
	go test ./...
	@echo "==> go test -race ./..."
	go test -race ./...
	@echo "==> govulncheck ./..."
	govulncheck ./...
	@echo "==> regression pack OK"
