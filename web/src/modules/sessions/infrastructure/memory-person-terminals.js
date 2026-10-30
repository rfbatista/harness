// PersonTerminalGateway in memory, for page tests and web/dev pages: terminals
// that open on request, a terminal socket that echoes (a memoryTerminals), and
// hooks to end one, restart the server or refuse the next open as the server would.

import { memoryTerminals } from "./memory-terminals.js";
import { StructuredError } from "../../../shared/domain/errors.js";

export function memoryPersonTerminals({ greeting = "% " } = {}) {
  let n = 0;
  /** @type {import("../domain/ports.js").PersonTerminal[]} */
  let open = [];
  let refusal = null;
  const followers = new Set();
  const attached = memoryTerminals({ greeting });
  const opened = [];
  const closed = [];

  /** @type {import("../domain/ports.js").PersonTerminalGateway} */
  const gateway = {
    async list(sessionId) {
      return open.filter((t) => t.sessionId === sessionId);
    },
    async open(sessionId, at) {
      opened.push({ sessionId, ...at });
      if (refusal) {
        const r = refusal;
        refusal = null;
        throw new StructuredError(r.code, r.message, r.status);
      }
      const t = Object.freeze({ id: `pt_${++n}`, sessionId, root: at.root, dir: at.dir, label: at.dir || (at.root === "resources" ? "Resources" : "Worktree"), openedAt: new Date(n * 1000) });
      open = [...open, t];
      return t;
    },
    async close(id) {
      closed.push(id);
      open = open.filter((t) => t.id !== id);
    },
    followEnded(sessionId, onEnded) {
      const f = { sessionId, onEnded };
      followers.add(f);
      return () => followers.delete(f);
    },
  };

  return {
    gateway,
    /** The terminal socket: a TerminalGateway for these ids. */
    terminals: attached.gateway,
    attached,
    opened,
    closed,
    /** The next open is refused with this code. */
    refuseNext(code, message, status = 403) {
      refusal = { code, message, status };
    },
    /** The server ended a terminal: its shell exits and the session's followers are told. */
    end(id, reason, message) {
      const t = open.find((x) => x.id === id);
      open = open.filter((x) => x.id !== id);
      attached.exit(id, 0);
      for (const f of followers) if (!t || f.sessionId === t.sessionId) f.onEnded({ id, sessionId: t?.sessionId ?? f.sessionId, reason, message });
    },
    /** The harness restarted: every terminal is gone, and nobody was told. */
    restart() {
      for (const t of open) attached.drop(t.id);
      open = [];
    },
  };
}
