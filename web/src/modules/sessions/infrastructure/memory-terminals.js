// TerminalGateway in memory: a terminal that greets and echoes what is typed,
// for page tests and web/dev pages. It records everything sent to it.

import { toKeyEvent } from "./terminal-keys.js";

const encoder = new TextEncoder();

/**
 * @param {{ greeting?: string, history?: string[] }} [options]
 *        history: lines that scrolled off before attaching, oldest first; the snapshot's scrollback
 */
export function memoryTerminals({ greeting = "claude is ready", history = [] } = {}) {
  /** Per session: what the page sent. */
  const sent = new Map();
  const open = new Map();

  const record = (id, message) => sent.set(id, [...(sent.get(id) ?? []), message]);

  /** @type {import("../domain/ports.js").TerminalGateway} */
  const gateway = {
    attach(sessionId, handlers) {
      open.set(sessionId, handlers);
      queueMicrotask(() => {
        handlers.onOpen?.();
        handlers.onSnapshot({
          screen: greeting,
          scrollback: history.join("\n"),
          cursorX: greeting.length,
          cursorY: 0,
          altScreen: false,
          cols: 80,
          rows: 24,
          title: "",
          log: false,
        });
      });
      return {
        key(domEvent) {
          const key = toKeyEvent(domEvent);
          if (!key) return false;
          record(sessionId, { type: "key", key });
          const echo = key.text ?? (key.code === 13 ? "\r\n" : "");
          if (echo) handlers.onOutput(encoder.encode(echo));
          return true;
        },
        paste(text) {
          record(sessionId, { type: "paste", text });
          handlers.onOutput(encoder.encode(text));
        },
        resize(size) {
          record(sessionId, { type: "resize", size });
        },
        resync() {
          record(sessionId, { type: "resync" });
        },
        close() {
          open.delete(sessionId);
          record(sessionId, { type: "close" });
        },
      };
    },
  };

  return {
    gateway,
    sent,
    /** Ends the process behind a session's terminal, as the server would. */
    exit(sessionId, code = 0) {
      const h = open.get(sessionId);
      h?.onExit?.(code);
      h?.onClosed?.();
      open.delete(sessionId);
    },
    print(sessionId, text) {
      open.get(sessionId)?.onOutput(encoder.encode(text));
    },
  };
}
