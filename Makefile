# ctm Makefile — UI build, Go build, and dev orchestration.
#
# Step 8 scope: `make ui` produces the React bundle and copies it into
# internal/serve/dist/ so Go can //go:embed it (sibling required because
# go:embed rejects parent-relative paths).

UI_DIR        := ui
UI_DIST       := $(UI_DIR)/dist
EMBED_DIST    := internal/serve/dist

.PHONY: ui build dev clean help

help:
	@echo "Targets:"
	@echo "  ui     — install + build React UI, sync to $(EMBED_DIST)"
	@echo "  build  — make ui && go build ./..."
	@echo "  dev    — pnpm --prefix ui dev + go run . serve in parallel"
	@echo "  clean  — remove $(UI_DIST) and $(EMBED_DIST)"

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
