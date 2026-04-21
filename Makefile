# ctm Makefile — UI build, Go build, and dev orchestration.
#
# Step 8 scope: `make ui` produces the React bundle and copies it into
# internal/serve/dist/ so Go can //go:embed it (sibling required because
# go:embed rejects parent-relative paths).

UI_DIR        := ui
UI_DIST       := $(UI_DIR)/dist
EMBED_DIST    := internal/serve/dist

.PHONY: ui build dev clean help e2e regression

help:
	@echo "Targets:"
	@echo "  ui         — install + build React UI, sync to $(EMBED_DIST)"
	@echo "  build      — make ui && go build ./..."
	@echo "  dev        — pnpm --prefix ui dev + go run . serve in parallel"
	@echo "  e2e        — Playwright tests against vite preview, mocked API surface"
	@echo "  regression — full pre-merge pack: go build/test/race/vuln + ui tsc/vitest/audit/e2e"
	@echo "  clean      — remove $(UI_DIST) and $(EMBED_DIST)"

ui:
	@echo "==> pnpm install"
	cd $(UI_DIR) && pnpm install --frozen-lockfile
	@echo "==> vite build"
	cd $(UI_DIR) && pnpm build
	@echo "==> rsync $(UI_DIST)/ → $(EMBED_DIST)/"
	mkdir -p $(EMBED_DIST)
	rsync -a --delete $(UI_DIST)/ $(EMBED_DIST)/

build: ui
	@echo "==> go build"
	go build -trimpath ./...

# Dev: run pnpm dev (Vite proxies to :37778) and go run . serve in parallel.
# Trap SIGINT so Ctrl-C tears down both.
dev:
	@trap 'kill 0' INT TERM EXIT; \
	pnpm --prefix $(UI_DIR) dev & \
	go run . serve & \
	wait

clean:
	rm -rf $(UI_DIST) $(EMBED_DIST)

# Playwright E2E. Uses Chromium from ~/.cache/ms-playwright (installed via
# `pnpm exec playwright install chromium`). Mocks /api + /events at page
# level so tests don't need a running daemon or fixture DB. Run
# `pnpm --prefix ui exec playwright install chromium` once before first run.
e2e:
	@echo "==> pnpm build (needed for vite preview)"
	cd $(UI_DIR) && pnpm build
	@echo "==> playwright test"
	cd $(UI_DIR) && pnpm exec playwright test

# Unified regression pack. Runs everything a PR must clear before merge.
# Fails fast — first non-zero exit stops the run. Total wall time on this
# machine is ~60-90s depending on Go cache state.
#
# Contract: every shipped bug fix or new feature adds a test case that
# executes under one of these steps (unit / vitest / e2e). The pack grows;
# it does not get replaced. See ui/e2e/README.md.
regression:
	@echo "==> go build ./..."
	go build ./...
	@echo "==> go test ./..."
	go test ./...
	@echo "==> go test -race ./internal/serve/..."
	go test -race ./internal/serve/...
	@echo "==> govulncheck ./..."
	govulncheck ./...
	@echo "==> pnpm -C ui tsc --noEmit"
	pnpm -C $(UI_DIR) exec tsc --noEmit
	@echo "==> pnpm -C ui test (vitest)"
	pnpm -C $(UI_DIR) test
	@echo "==> pnpm audit (High/Critical fails; Medium/Low reported)"
	cd $(UI_DIR) && pnpm audit --audit-level=high
	@echo "==> make e2e"
	$(MAKE) e2e
	@echo "==> regression pack OK"
