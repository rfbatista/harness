// FilesGateway over the harness HTTP API (the virtual file system contract's
// /api/sessions/:id/files...) and the project feed's session_files_root and
// session status. The feed's messages are bare `data:` lines.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { AGENT_STATES } from "../domain/files.js";
import { toFile } from "./dto.js";

const bad = (msg) => {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected files data: ${msg}.`);
};
const str = (v) => (typeof v === "string" ? v : "");

/** @returns {import("../domain/files.js").FilesRoot} */
export function toRoot(dto) {
  if (!dto || typeof dto.root !== "string") bad("root without a name");
  return Object.freeze({ root: dto.root, label: str(dto.label) || dto.root, available: dto.available === true, note: str(dto.note) });
}

export function toRoots(dto) {
  if (!dto || !Array.isArray(dto.roots)) bad("roots without a list");
  const state = AGENT_STATES.includes(dto.agent_state) ? dto.agent_state : "working";
  return Object.freeze({ sessionId: str(dto.session_id), agentState: state, roots: Object.freeze(dto.roots.map(toRoot)) });
}

/** @returns {import("../domain/files.js").FilesEntry} */
export function toEntry(dto) {
  if (!dto || typeof dto.name !== "string" || !["dir", "file", "symlink"].includes(dto.kind)) bad("entry");
  return Object.freeze({ name: dto.name, kind: dto.kind, ignored: dto.ignored === true, size: Number.isInteger(dto.size) ? dto.size : 0, target: str(dto.target) });
}

export function toEntries(dto) {
  if (!dto || !Array.isArray(dto.entries)) bad("entries without a list");
  return Object.freeze({ root: str(dto.root), dir: str(dto.dir), entries: Object.freeze(dto.entries.map(toEntry)), next: str(dto.next) });
}

/** The session status of a feed message, or null for every other message. */
export function toSessionState(msg) {
  const s = msg?.session;
  if (!s || typeof s.id !== "string") return null;
  switch (s.status) {
    case "done":
    case "failed":
    case "stopped":
      return { sessionId: s.id, agentState: "ended" };
    case "waiting_approval":
      return { sessionId: s.id, agentState: "waiting" };
    case "idle":
    case "paused":
      return { sessionId: s.id, agentState: "idle" };
    default:
      return { sessionId: s.id, agentState: "working" };
  }
}

/** A feed message's session_files_root, or null for every other message. */
export function toRootChange(msg) {
  const c = msg?.session_files_root;
  if (!c) return null;
  if (typeof c.session_id !== "string" || typeof c.root !== "string") bad("session_files_root without a session or a root");
  return Object.freeze({ sessionId: c.session_id, root: c.root, available: c.available === true, note: str(c.note) });
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 */
export function filesGateway(api, feed) {
  const base = (id) => `/sessions/${encodeURIComponent(id)}/files`;
  return {
    async roots(sessionId, signal) {
      return toRoots(await api.get(base(sessionId), {}, signal));
    },
    async entries(sessionId, { root, dir, after, limit }, signal) {
      return toEntries(await api.get(`${base(sessionId)}/entries`, { root, dir, after: after || undefined, limit: limit ? String(limit) : undefined }, signal));
    },
    async file(sessionId, root, path, signal) {
      return toFile(await api.get(`${base(sessionId)}/file`, { root, path }, signal));
    },
    /** Every root change and status of the project's sessions until the returned function is called. */
    follow(projectId, { onRoot, onState, onStatus }) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (msg) => {
          try {
            const root = toRootChange(msg);
            if (root) return onRoot(root);
            const state = toSessionState(msg);
            if (state) onState(state);
          } catch {
            // a message we cannot read is dropped; the next change says it again
          }
        },
        onStatus,
      });
    },
  };
}
