# operators-mcp — MCP server + Architecture Designer UI
# See specs/001-go-react-ui-bridge/quickstart.md for usage.

# Load environment variables from .env when available.
-include .env
export

.PHONY: help build build-tui tui run run-api dev-server test clean web-install web-build web-dev copy-ui deps air stop-api genkit-qwen3-tools genkit-postman-agent build-crap crap install-crap update-crap build-linkedin-mcp linkedin-mcp linkedin-login linkedin-mcpb build-claude-pane claude-pane

BINARY   := bin/server
TUI_BINARY := bin/coding-pool-tui
CRAP_BINARY := bin/crap
CLAUDE_PANE_BINARY := bin/claude-pane
LINKEDIN_BINARY := bin/linkedin-mcp
LINKEDIN_MCPB_DIR := bin/linkedin-mcpb
LINKEDIN_MCPB := bin/linkedin-mcp.mcpb
WEB_DIR  := web
UI_STATIC := internal/adapter/in/ui/static

# Default target
help:
	@echo "operators-mcp — targets:"
	@echo "  make build       — build web UI, copy to embed dir, build Go binary ($(BINARY))"
	@echo "  make run         — run MCP server (:8081) + HTTP server (:8080 for UI and API). Requires 'make build' first."
	@echo "  make run-api     — run Go API/MCP from source using .env (no embedded UI build; use with Flutter)"
	@echo "  make dev-server  — run Go server with --dev (run 'make web-dev' in another terminal for hot-reload)"
	@echo "  make genkit-qwen3-tools — run Genkit + Ollama Qwen3 tool-calling test app"
	@echo "  make genkit-postman-agent — run Genkit Postman MCP agent app"
	@echo "  make web-dev     — start Vite dev server (port 5173)"
	@echo "  make air         — hot-reload Go API with Air + .env (use with Flutter in another terminal)"
	@echo "  make stop-api    — stop Air/tmp/main and free ports 8080 (HTTP) and 8081 (MCP)"
	@echo "  make test        — run Go tests (contract + integration)"
	@echo "  make build-crap  — build the standalone CRAP analyzer ($(CRAP_BINARY))"
	@echo "  make crap        — run the CRAP analyzer (make crap ARGS='--cover=cover.out ./internal/...')"
	@echo "  make install-crap — install crap globally via 'go install' (into \$$GOBIN or \$$GOPATH/bin)"
	@echo "  make update-crap — rebuild + reinstall the global crap from the current source"
	@echo "  make linkedin-login — open a browser window to log in to LinkedIn (once, before linkedin-mcp)"
	@echo "  make linkedin-mcp — run the LinkedIn MCP server at http://localhost:9090/mcp (ARGS='--headless=false')"
	@echo "  make build-linkedin-mcp — build the LinkedIn MCP server ($(LINKEDIN_BINARY))"
	@echo "  make linkedin-mcpb — pack the LinkedIn MCP server as a Claude Desktop extension ($(LINKEDIN_MCPB), macOS)"
	@echo "  make claude-pane — terminal workbench: projects → tasks → agent sessions as claude panes (needs make air; ARGS='--api URL')"
	@echo "  make build-claude-pane — build the claude pane host ($(CLAUDE_PANE_BINARY))"
	@echo "  make clean       — remove bin/, web/dist/, $(UI_STATIC)/"
	@echo "  make deps        — install Go deps + web npm deps"
	@echo ""
	@echo "Production: make deps && make build && make run"
	@echo "Development (Flutter): make air (terminal 1), make web (terminal 2)"
	@echo "  API base URL (Flutter): http://localhost:8080/api (override with --dart-define=API_BASE_URL=...)"

# Install Go and web dependencies
deps:
	go mod download
	$(MAKE) web-install

web:
	flutter run -d web

# Copy web/dist into internal/ui/static for Go embed (required before 'go build')
copy-ui:
	@mkdir -p $(UI_STATIC)
	@cp -r $(WEB_DIR)/dist/* $(UI_STATIC)/
	@echo "Copied $(WEB_DIR)/dist/ -> $(UI_STATIC)/"

# Build production binary: web build + copy + go build
build: build-tui
	go build -o $(BINARY) ./cmd/server
	@echo "Built $(BINARY) (production, UI embedded)"

# Build the terminal UI binary (full server + Bubble Tea front-end)
build-tui:
	go build -o $(TUI_BINARY) ./cmd/tui
	@echo "Built $(TUI_BINARY)"

# Run the terminal UI (owns data.db; stop `make air` first). Logs go to ~/.coding-pool/tui.log
tui:
	go run ./cmd/tui

# Build the standalone CRAP (Change Risk Anti-Patterns) analyzer.
build-claude-pane:
	go build -o $(CLAUDE_PANE_BINARY) ./cmd/claude-pane
	@echo "Built $(CLAUDE_PANE_BINARY)"

# Terminal workbench over the running server (start `make air` first).
claude-pane:
	go run ./cmd/claude-pane $(ARGS)

build-crap:
	go build -o $(CRAP_BINARY) ./cmd/crap
	@echo "Built $(CRAP_BINARY)"

# Run the CRAP analyzer from source, e.g.:
#   make crap ARGS='--cover=cover.out ./internal/...'
crap:
	go run ./cmd/crap $(ARGS)

# Install crap globally via 'go install' — puts the binary in $GOBIN, or
# $GOPATH/bin, or ~/go/bin, whichever `go env` resolves. Make sure that
# directory is on your PATH so the 'crap' command is available anywhere.
# Under asdf-managed Go, GOBIN sits under the asdf install dir, so this also
# reshims asdf's golang plugin to pick up the new/updated binary.
install-crap:
	go install ./cmd/crap
	@command -v asdf >/dev/null 2>&1 && asdf reshim golang 2>/dev/null || true
	@echo "Installed crap to $$(go env GOBIN 2>/dev/null | grep . || echo $$(go env GOPATH)/bin)"

# Update the globally installed crap to match the current source tree.
# Same as install-crap — 'go install' always rebuilds from source — kept as
# a separate target so 'make update-crap' reads naturally after a pull.
update-crap: install-crap
	@echo "crap is up to date."

# Standalone LinkedIn MCP server (see cmd/linkedin-mcp/README.md).
build-linkedin-mcp:
	go build -o $(LINKEDIN_BINARY) ./cmd/linkedin-mcp
	@echo "Built $(LINKEDIN_BINARY)"

# Opens a visible browser window; log in by hand. The session is kept in
# ~/.config/linkedin-mcp/browser-profile.
linkedin-login:
	go run ./cmd/linkedin-mcp login $(ARGS)

linkedin-mcp:
	go run ./cmd/linkedin-mcp serve $(ARGS)

# MCP Bundle for Claude Desktop: a universal macOS binary (arm64 + amd64) next
# to cmd/linkedin-mcp/mcpb/manifest.json, validated and packed with the mcpb CLI.
linkedin-mcpb:
	rm -rf $(LINKEDIN_MCPB_DIR) && mkdir -p $(LINKEDIN_MCPB_DIR)/server
	GOOS=darwin GOARCH=arm64 go build -o $(LINKEDIN_MCPB_DIR)/linkedin-mcp-arm64 ./cmd/linkedin-mcp
	GOOS=darwin GOARCH=amd64 go build -o $(LINKEDIN_MCPB_DIR)/linkedin-mcp-amd64 ./cmd/linkedin-mcp
	lipo -create -output $(LINKEDIN_MCPB_DIR)/server/linkedin-mcp $(LINKEDIN_MCPB_DIR)/linkedin-mcp-arm64 $(LINKEDIN_MCPB_DIR)/linkedin-mcp-amd64
	rm $(LINKEDIN_MCPB_DIR)/linkedin-mcp-arm64 $(LINKEDIN_MCPB_DIR)/linkedin-mcp-amd64
	cp cmd/linkedin-mcp/mcpb/manifest.json $(LINKEDIN_MCPB_DIR)/
	npx -y @anthropic-ai/mcpb validate $(LINKEDIN_MCPB_DIR)/manifest.json
	npx -y @anthropic-ai/mcpb pack $(LINKEDIN_MCPB_DIR) $(LINKEDIN_MCPB)
	@echo "Built $(LINKEDIN_MCPB) — double-click it (or drag it into Claude Desktop > Settings > Extensions) to install"

# Run HTTP server (UI at /, API at /api, MCP at /mcp).
run: build
	./$(BINARY)

# Run Go API/MCP using .env (recommended with the Flutter app).
run-api:
	go run ./cmd/server

# Run Go server in dev mode (proxies ui://designer to Vite; start Vite with 'make web-dev' first)
dev-server:
	go run ./cmd/server --dev

# Stop dev API/MCP processes (Air child, stale go run, or anything on default ports).
stop-api:
	@./scripts/dev/kill-stale-api.sh
	@sleep 0.3
	@if lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "Port 8080 still in use:"; lsof -nP -iTCP:8080 -sTCP:LISTEN; exit 1; \
	fi
	@echo "API ports 8080/8081 are free."

# Hot-reload Go API/MCP using Air (.env is exported by Makefile).
air:
	@command -v air >/dev/null 2>&1 || { echo "Install Air: go install github.com/air-verse/air@latest"; exit 1; }
	@if lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "Port 8080 in use; stopping stale API processes..."; \
		$(MAKE) stop-api || true; \
	fi
	@if lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "Port 8080 is still in use. Stop the other process or run 'make stop-api'."; \
		lsof -nP -iTCP:8080 -sTCP:LISTEN; \
		exit 1; \
	fi
	air

# Run all Go tests
test:
	go test ./...

# Run tests with race detector
test-race:
	go test -race ./...

# Remove build artifacts
clean:
	rm -rf bin
	rm -rf $(WEB_DIR)/dist
	rm -rf $(UI_STATIC)
	@echo "Cleaned bin/, $(WEB_DIR)/dist/, $(UI_STATIC)/"
