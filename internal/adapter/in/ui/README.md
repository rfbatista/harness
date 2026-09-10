# Designer resource and dev proxy

Serves the Architecture Designer at `ui://designer` (embed or dev proxy).

## UI ↔ backend (tool calls)

When the designer is loaded in a browser or webview, it cannot call MCP over stdio directly. The MCP host (e.g. Cursor, Claude Desktop) is responsible for exposing tool calls to the embedded UI—for example via a bridge (postMessage, injected API, or host-provided RPC). This server exposes all blueprint tools (list_tree, list_matching_paths, zone CRUD) via MCP; no separate HTTP endpoint is provided in this implementation. The UI should use whatever mechanism the host provides to invoke tools.
