# coding_pool — the server (HTTP API on :8080, MCP on :8081) and tui-client,
# its terminal client. `make` lists the targets.

# .env configures the server (HTTP_ADDR, DB_PATH, CLAUDE_BIN, ...); every
# variable in it reaches the recipes.
-include .env
export

.DEFAULT_GOAL := help

BIN := bin

# tui-client settings, e.g. `make tui-client RUN=tui API=http://host:8080`.
# Empty ones keep the client's defaults: the local server, sessions run by it.
API       ?=
RUN       ?=
TOKEN     ?=
TUI_SHELL ?=
TUI_FLAGS = $(if $(API),--api $(API)) $(if $(RUN),--run $(RUN)) \
            $(if $(TOKEN),--token $(TOKEN)) $(if $(TUI_SHELL),--shell $(TUI_SHELL))

.PHONY: help air stop-api server run build build-server build-tui-client \
        tui-client tui-client-bin install-tui-client update-tui-client uninstall-tui-client \
        test test-race vet check deps clean \
        crap build-crap install-crap update-crap \
        linkedin-login linkedin-mcp build-linkedin-mcp linkedin-mcpb

help: ## list the targets
	@awk 'BEGIN {FS = ":.*## "} \
		/^# ---/ {h = substr($$0, 7); sub(/ -+$$/, "", h); printf "\n%s\n", h} \
		/^[a-zA-Z_-]+:.*## / {printf "  make %-20s %s\n", $$1, $$2}' $(firstword $(MAKEFILE_LIST))
	@echo
	@echo "tui-client settings: API=<url> RUN=server|tui TOKEN=<token> TUI_SHELL=direct|login ARGS=<more flags>"

# --- Server ------------------------------------------------------------------

air: ## run the server with hot reload (Air); start here
	@command -v air >/dev/null 2>&1 || { echo "Install Air: go install github.com/air-verse/air@latest"; exit 1; }
	@if lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "Port 8080 in use; stopping stale server processes..."; \
		$(MAKE) --no-print-directory stop-api || true; \
	fi
	@if lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "Port 8080 is still in use. Stop the other process or run 'make stop-api'."; \
		lsof -nP -iTCP:8080 -sTCP:LISTEN; \
		exit 1; \
	fi
	air

server: ## run the server from source, without hot reload
	go run ./cmd/server $(ARGS)

run: build-server ## build the server binary and run it
	./$(BIN)/server $(ARGS)

stop-api: ## stop stale server processes and free ports 8080/8081
	@./scripts/dev/kill-stale-api.sh
	@sleep 0.3
	@if lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "Port 8080 still in use:"; lsof -nP -iTCP:8080 -sTCP:LISTEN; exit 1; \
	fi
	@echo "Ports 8080/8081 are free."

# --- tui-client (needs the server running) --------------------------------------

tui-client: ## run tui-client from source (settings below)
	go run ./cmd/tui-client $(TUI_FLAGS) $(ARGS)

tui-client-bin: build-tui-client ## build tui-client and run the binary
	./$(BIN)/tui-client $(TUI_FLAGS) $(ARGS)

# go install puts tui-client in $GOBIN, else $GOPATH/bin — the directory
# `make` prints; it must be on PATH. Under asdf-managed Go it also reshims, so
# the new binary is picked up. Run from anywhere afterwards:
#   tui-client [--api URL] [--run server|tui] [--token TOKEN]
GOBIN_DIR = $$(go env GOBIN | grep . || echo $$(go env GOPATH)/bin)

install-tui-client: ## install tui-client on this machine (go install), to run it from anywhere
	go install ./cmd/tui-client
	@command -v asdf >/dev/null 2>&1 && asdf reshim golang 2>/dev/null || true
	@echo "Installed $(GOBIN_DIR)/tui-client"
	@command -v tui-client >/dev/null 2>&1 || echo "$(GOBIN_DIR) is not on your PATH: add it to run tui-client from anywhere"

update-tui-client: install-tui-client ## upgrade the installed tui-client to the current source

uninstall-tui-client: ## remove the installed tui-client
	rm -f $(GOBIN_DIR)/tui-client
	@command -v asdf >/dev/null 2>&1 && asdf reshim golang 2>/dev/null || true
	@echo "Removed $(GOBIN_DIR)/tui-client"

# --- Build -------------------------------------------------------------------

build: build-server build-tui-client ## build the server and tui-client into bin/

build-server: ## build bin/server
	go build -o $(BIN)/server ./cmd/server

build-tui-client: ## build bin/tui-client
	go build -o $(BIN)/tui-client ./cmd/tui-client

# --- Quality -----------------------------------------------------------------

# ./... would include agents/, whose example files are not part of the module.
PKGS := ./cmd/... ./internal/... ./tests/...

test: ## run the tests
	go test $(PKGS)

test-race: ## run the tests with the race detector
	go test -race $(PKGS)

vet: ## run go vet
	go vet $(PKGS)

check: ## what to run before calling work done: build, vet, race tests
	go build $(PKGS)
	go vet $(PKGS)
	go test -race $(PKGS)

deps: ## download Go modules
	go mod download

clean: ## remove build output (bin/, tmp/)
	rm -rf $(BIN) tmp

# --- Tools: CRAP analyzer ------------------------------------------------------

crap: ## run the CRAP analyzer, e.g. ARGS='--cover=cover.out ./internal/...'
	go run ./cmd/crap $(ARGS)

build-crap: ## build bin/crap
	go build -o $(BIN)/crap ./cmd/crap

# Puts crap in $GOBIN (or $GOPATH/bin); that directory must be on PATH. Under
# asdf-managed Go it also reshims, so the new binary is picked up.
install-crap: ## install crap globally with go install
	go install ./cmd/crap
	@command -v asdf >/dev/null 2>&1 && asdf reshim golang 2>/dev/null || true
	@echo "Installed crap to $$(go env GOBIN 2>/dev/null | grep . || echo $$(go env GOPATH)/bin)"

update-crap: install-crap ## reinstall crap from the current source

# --- Tools: LinkedIn MCP server (see cmd/linkedin-mcp/README.md) -----------------

linkedin-login: ## log in to LinkedIn in a browser window (once)
	go run ./cmd/linkedin-mcp login $(ARGS)

linkedin-mcp: ## run the LinkedIn MCP server at http://localhost:9090/mcp
	go run ./cmd/linkedin-mcp serve $(ARGS)

build-linkedin-mcp: ## build bin/linkedin-mcp
	go build -o $(BIN)/linkedin-mcp ./cmd/linkedin-mcp

LINKEDIN_MCPB_DIR := $(BIN)/linkedin-mcpb

# A universal macOS binary next to the manifest, validated and packed with the
# mcpb CLI. Install by double-clicking the .mcpb, or Claude Desktop > Settings >
# Extensions.
linkedin-mcpb: ## pack the LinkedIn MCP server as a Claude Desktop extension (macOS)
	rm -rf $(LINKEDIN_MCPB_DIR) && mkdir -p $(LINKEDIN_MCPB_DIR)/server
	GOOS=darwin GOARCH=arm64 go build -o $(LINKEDIN_MCPB_DIR)/linkedin-mcp-arm64 ./cmd/linkedin-mcp
	GOOS=darwin GOARCH=amd64 go build -o $(LINKEDIN_MCPB_DIR)/linkedin-mcp-amd64 ./cmd/linkedin-mcp
	lipo -create -output $(LINKEDIN_MCPB_DIR)/server/linkedin-mcp $(LINKEDIN_MCPB_DIR)/linkedin-mcp-arm64 $(LINKEDIN_MCPB_DIR)/linkedin-mcp-amd64
	rm $(LINKEDIN_MCPB_DIR)/linkedin-mcp-arm64 $(LINKEDIN_MCPB_DIR)/linkedin-mcp-amd64
	cp cmd/linkedin-mcp/mcpb/manifest.json $(LINKEDIN_MCPB_DIR)/
	npx -y @anthropic-ai/mcpb validate $(LINKEDIN_MCPB_DIR)/manifest.json
	npx -y @anthropic-ai/mcpb pack $(LINKEDIN_MCPB_DIR) $(BIN)/linkedin-mcp.mcpb
	@echo "Built $(BIN)/linkedin-mcp.mcpb"
