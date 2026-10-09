// ResourceGateway in memory: the fake for page tests and the data source for
// web/dev pages. It obeys the same contract as the real gateway
// (../testing/resource-contract.js) and mirrors the server's rules: kind
// from the MIME type, titles and bodies trimmed, the 25 MiB limit, a
// session's comment deleted but never edited, the change on the project
// feed for every write. Sessions writing comments, starting and being
// deleted are played from outside (sessionAdds…, startSession…): the API
// has no way to do either.
//
// Uploads finish on their own a tick after they start. With holdUploads, each
// waits in `uploads` until the test (or the dev console) moves it on.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { MAX_COMMENT } from "../domain/comments.js";
import { defaultUploadTitle } from "../domain/items.js";
import { linkProblem, MAX_RESOURCE_BYTES, runeCount, titleProblem } from "../domain/resources.js";
import { toResourcesSeed } from "./resource-dto.js";

const err = (code, message, status) => new StructuredError(code, message, status);
const taskNotFound = () => err(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
const resourceNotFound = () => err(Codes.RESOURCE_NOT_FOUND, "resource not found", 404);
const commentNotFound = () => err(Codes.COMMENT_NOT_FOUND, "comment not found", 404);
const commentNotYours = () => err(Codes.COMMENT_NOT_YOURS, "a session wrote this comment; only it can edit it", 403);

const EXTENSION_TYPES = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", svg: "image/svg+xml", pdf: "application/pdf", txt: "text/plain", json: "application/json", m4a: "audio/mp4", mp3: "audio/mpeg" };

/** The server's fallback: the part's type, else the extension's, else bytes. */
function mimeOf(file) {
  if (file.type && file.type !== "application/octet-stream") return file.type;
  const ext = /\.([^.]+)$/.exec(file.name ?? "")?.[1]?.toLowerCase() ?? "";
  return EXTENSION_TYPES[ext] ?? "application/octet-stream";
}

const kindOf = (mime) => (mime.startsWith("image/") ? "image" : mime.startsWith("audio/") ? "audio" : "file");

/**
 * @param {{
 *   tickets?: { id: string, projectId: string }[],
 *   resources?: import("../domain/resources.js").Resource[],
 *   comments?: import("../domain/comments.js").Comment[],
 *   sessions?: { id: string, ticketId: string }[],
 *   now?: () => Date,
 *   holdUploads?: boolean,
 * }} [world]
 */
export function memoryResources({ tickets = [{ id: "t1", projectId: "p1" }], resources = [], comments = [], sessions = [], now = () => new Date(), holdUploads = false } = {}) {
  let nextId = 1;
  const projects = new Map(tickets.map((t) => [t.id, t.projectId]));
  /** Insertion order is creation order: the lists are oldest first. */
  const resourceStore = new Map(resources.map((r) => [r.id, r]));
  const commentStore = new Map(comments.map((c) => [c.id, c]));
  /** Session id → its task. */
  const sessionStore = new Map(sessions.map((x) => [x.id, x.ticketId]));
  const followers = new Set();
  /** Uploads waiting on the test, when held. */
  const uploads = [];

  function emit(projectId, change) {
    for (const f of [...followers]) if (f.projectId === projectId) f.onChange(change);
  }

  function requireTicket(ticketId) {
    const projectId = projects.get(ticketId);
    if (!projectId) throw taskNotFound();
    return projectId;
  }

  /** A comment's body as the server takes it: trimmed, 1–10,000 runes. */
  function commentBody(body) {
    const text = (body ?? "").trim();
    if (!text || runeCount(text) > MAX_COMMENT) throw err(Codes.INVALID_INPUT, "body must be 1–10000 characters", 400);
    return text;
  }

  /** A write is a new updated_at, even when the clock has not moved. */
  function later(than) {
    return new Date(Math.max(now().getTime(), than.getTime() + 1));
  }

  function store(resource) {
    resourceStore.set(resource.id, resource);
    emit(resource.projectId, { kind: "resource", resource });
    return resource;
  }

  function storeComment(comment) {
    commentStore.set(comment.id, comment);
    emit(comment.projectId, { kind: "comment", comment });
    return comment;
  }

  function newResource(ticketId, fields) {
    const at = now();
    const id = `res-${nextId++}`;
    return Object.freeze({ id, ticketId, projectId: projects.get(ticketId), createdAt: at, updatedAt: at, mime: "", sizeBytes: 0, url: "", contentUrl: "", ...fields, ...(fields.kind === "link" ? {} : { contentUrl: `/api/ticket_resources/${id}/content` }) });
  }

  function finishUpload(ticketId, file, title) {
    requireTicket(ticketId); // the task may be gone by the time the bytes are in
    if (file.size > MAX_RESOURCE_BYTES) throw err(Codes.RESOURCE_TOO_LARGE, "the file is over 25 MiB", 413);
    if (file.size === 0) throw err(Codes.INVALID_INPUT, "the file is empty", 400);
    const t = title.trim() || defaultUploadTitle(file.name);
    if (titleProblem(t)) throw err(Codes.INVALID_INPUT, "title must be 1–200 characters", 400);
    const mime = mimeOf(file);
    return store(newResource(ticketId, { kind: kindOf(mime), title: t, mime, sizeBytes: file.size }));
  }

  function newComment(ticketId, text, by) {
    const at = now();
    return storeComment(Object.freeze({ id: `com-${nextId++}`, ticketId, projectId: projects.get(ticketId), ...by, body: text, createdAt: at, updatedAt: at }));
  }

  function editBody(c, text) {
    if (text === c.body) return c; // a no-op edit keeps updated_at
    return storeComment(Object.freeze({ ...c, body: text, updatedAt: later(c.updatedAt) }));
  }

  function removeStoredComment(c) {
    commentStore.delete(c.id);
    emit(c.projectId, { kind: "comment-deleted", id: c.id, ticketId: c.ticketId });
  }

  /** @type {import("../domain/ports.js").ResourceGateway} */
  const gateway = {
    decodeSeed: toResourcesSeed,

    async listResources(ticketId) {
      requireTicket(ticketId);
      return [...resourceStore.values()].filter((r) => r.ticketId === ticketId);
    },

    upload({ ticketId, file, title = "", onProgress = () => {}, signal }) {
      return new Promise((resolve, reject) => {
        const abort = () => reject(new DOMException("The upload was cancelled.", "AbortError"));
        if (signal?.aborted) return abort();
        try {
          requireTicket(ticketId);
        } catch (e) {
          return reject(e);
        }
        let settled = false;
        const job = {
          ticketId,
          file,
          title,
          progress(loaded) {
            if (!settled) onProgress(loaded, file.size);
          },
          finish() {
            if (settled) return;
            settled = true;
            try {
              resolve(finishUpload(ticketId, file, title));
            } catch (e) {
              reject(e);
            }
          },
          fail(code = Codes.NETWORK) {
            if (settled) return;
            settled = true;
            reject(err(code, code === Codes.NETWORK ? "The harness server is not reachable." : "the upload failed"));
          },
        };
        signal?.addEventListener("abort", () => {
          if (settled) return;
          settled = true;
          const i = uploads.indexOf(job);
          if (i >= 0) uploads.splice(i, 1);
          abort();
        });
        if (holdUploads) {
          uploads.push(job);
          return;
        }
        queueMicrotask(() => {
          job.progress(file.size);
          job.finish();
        });
      });
    },

    async addLink({ ticketId, url, title = "" }) {
      requireTicket(ticketId);
      const u = (url ?? "").trim();
      if (linkProblem(u)) throw err(Codes.INVALID_INPUT, "url must be http or https with a host, at most 2048 characters", 400);
      const t = title.trim() || [...u].slice(0, 200).join("");
      if (titleProblem(t)) throw err(Codes.INVALID_INPUT, "title must be 1–200 characters", 400);
      return store(newResource(ticketId, { kind: "link", title: t, url: u }));
    },

    async retitle(resourceId, title) {
      const r = resourceStore.get(resourceId);
      if (!r) throw resourceNotFound();
      const t = (title ?? "").trim();
      if (titleProblem(t)) throw err(Codes.INVALID_INPUT, "title must be 1–200 characters", 400);
      if (t === r.title) return r;
      return store(Object.freeze({ ...r, title: t, updatedAt: later(r.updatedAt) }));
    },

    async removeResource(resourceId) {
      const r = resourceStore.get(resourceId);
      if (!r) throw resourceNotFound();
      resourceStore.delete(resourceId);
      emit(r.projectId, { kind: "resource-deleted", id: r.id, ticketId: r.ticketId });
    },

    async listComments(ticketId) {
      requireTicket(ticketId);
      return [...commentStore.values()].filter((c) => c.ticketId === ticketId);
    },

    async addComment(ticketId, body) {
      requireTicket(ticketId);
      return newComment(ticketId, commentBody(body), { author: "person", sessionId: "", agentName: "" });
    },

    async editComment(commentId, body) {
      const c = commentStore.get(commentId);
      if (!c) throw commentNotFound();
      if (c.author === "session") throw commentNotYours();
      return editBody(c, commentBody(body));
    },

    async removeComment(commentId) {
      const c = commentStore.get(commentId);
      if (!c) throw commentNotFound();
      removeStoredComment(c);
    },

    follow(projectId, onChange, onStatus) {
      const follower = { projectId, onChange, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  return {
    gateway,
    /** Held uploads, oldest first: each has progress(loaded), finish() and fail(code). */
    uploads,
    /** The task is deleted: its resources and comments go with it, with only the ticket's change on the feed. */
    deleteTask(ticketId) {
      const projectId = projects.get(ticketId);
      projects.delete(ticketId);
      for (const r of [...resourceStore.values()]) if (r.ticketId === ticketId) resourceStore.delete(r.id);
      for (const c of [...commentStore.values()]) if (c.ticketId === ticketId) commentStore.delete(c.id);
      emit(projectId, { kind: "task-deleted", id: ticketId });
    },
    /** A session on the task writes a comment, as its task tool would: on the feed, not through the gateway. */
    sessionAdds({ ticketId, sessionId, agentName = "", body }) {
      requireTicket(ticketId);
      return newComment(ticketId, commentBody(body), { author: "session", sessionId, agentName });
    },
    /** The session edits its comment. */
    sessionEdits(commentId, body) {
      const c = commentStore.get(commentId);
      if (!c) throw commentNotFound();
      return editBody(c, commentBody(body));
    },
    /** The session deletes its comment. */
    sessionDeletes(commentId) {
      const c = commentStore.get(commentId);
      if (!c) throw commentNotFound();
      removeStoredComment(c);
    },
    /** A session starts on the task: the feed says so. */
    startSession(ticketId, sessionId) {
      sessionStore.set(sessionId, ticketId);
      emit(projects.get(ticketId), { kind: "session", id: sessionId, ticketId });
    },
    /** The session's record is deleted: it is no longer listed on its task. */
    deleteSession(sessionId) {
      const ticketId = sessionStore.get(sessionId);
      sessionStore.delete(sessionId);
      emit(projects.get(ticketId), { kind: "session-deleted", id: sessionId });
    },
    resources: () => [...resourceStore.values()],
    comments: () => [...commentStore.values()],
    feedStatus(status) {
      for (const f of followers) f.onStatus(status);
    },
  };
}
