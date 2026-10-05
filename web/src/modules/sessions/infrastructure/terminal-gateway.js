// TerminalGateway over the server's attach socket,
// GET /api/sessions/:id/terminal (httpapi/terminal.go, ports.TerminalMessage):
// text frames carry snapshot / title / exit one way and key / paste / resize /
// resync the other; output arrives as binary frames, in order, between them.

import { toKeyEvent } from "./terminal-keys.js";

/**
 * @param {{
 *   base: string,
 *   path?: (id: string) => string,  the socket for an id; a session's terminal by default
 *   location?: { protocol: string, host: string },
 *   WebSocket?: typeof globalThis.WebSocket,
 * }} options
 * @returns {import("../domain/ports.js").TerminalGateway}
 */
export function terminalGateway({
  base,
  path = (id) => `/sessions/${encodeURIComponent(id)}/terminal`,
  location = globalThis.location,
  WebSocket = globalThis.WebSocket,
}) {
  const origin = `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}`;

  return {
    attach(sessionId, handlers) {
      const socket = new WebSocket(`${origin}${base}${path(sessionId)}`);
      socket.binaryType = "arraybuffer";
      let closed = false;

      const send = (message) => {
        if (socket.readyState === 1 /* OPEN */) socket.send(JSON.stringify(message));
      };

      socket.onopen = () => handlers.onOpen?.();
      socket.onmessage = (event) => {
        if (event.data instanceof ArrayBuffer) {
          handlers.onOutput(new Uint8Array(event.data));
          return;
        }
        let message;
        try {
          message = JSON.parse(event.data);
        } catch {
          return;
        }
        switch (message.type) {
          case "snapshot":
            handlers.onSnapshot(toSnapshot(message.snapshot));
            break;
          case "title":
            handlers.onTitle?.(message.title ?? "");
            break;
          case "exit":
            handlers.onExit?.(message.code ?? 0);
            break;
        }
      };
      socket.onclose = () => {
        if (!closed) handlers.onClosed?.();
        closed = true;
      };

      return {
        key(domEvent) {
          const key = toKeyEvent(domEvent);
          if (!key) return false;
          send({ type: "key", key });
          return true;
        },
        paste: (text) => send({ type: "paste", text }),
        resize: ({ cols, rows }) => send({ type: "resize", size: { cols, rows } }),
        resync: () => send({ type: "resync" }),
        close() {
          closed = true;
          socket.close();
        },
      };
    },
  };
}

/** ports.TerminalSnapshot → the domain's snapshot. */
export function toSnapshot(s = {}) {
  return {
    screen: s.screen ?? "",
    cursorX: s.cursor_x ?? 0,
    cursorY: s.cursor_y ?? 0,
    altScreen: s.alt_screen === true,
    cols: s.size?.cols ?? 80,
    rows: s.size?.rows ?? 24,
    title: s.title ?? "",
    log: s.log === true,
  };
}
