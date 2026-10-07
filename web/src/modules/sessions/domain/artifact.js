// Artifacts: what a session published for the developer to look at. Mirrors
// the Artifact resource of the server contract; no I/O, no Alpine, no DOM.

/**
 * @typedef {"page"|"image"|"video"|"url"|"file"} ArtifactKind
 * @typedef {"task"|"project"} ArtifactScope  project: it belongs to the project and outlives its session
 *
 * @typedef {object} Artifact
 * @property {string} id
 * @property {string} sessionId
 * @property {string} ticketId
 * @property {string} projectId
 * @property {ArtifactKind} kind
 * @property {string} title
 * @property {string} note       what changed in this revision; may be ""
 * @property {string} path       worktree-relative, POSIX separators; "" when kind = url
 * @property {string} url        only when kind = url; "" otherwise
 * @property {string} mime       "" when kind = url
 * @property {number} sizeBytes  0 when kind = url
 * @property {number} revision   1 on first publish; +1 on each re-publish of the same path or url
 * @property {ArtifactScope} scope  a move changes it and updatedAt, never the revision
 * @property {Date} createdAt
 * @property {Date} updatedAt
 * @property {string} src        where the browser loads it from: the server's view route (page, image, video, file) or the url itself (url)
 */

export const Kind = Object.freeze({ PAGE: "page", IMAGE: "image", VIDEO: "video", URL: "url", FILE: "file" });

export const KINDS = Object.freeze(Object.values(Kind));

export const Scope = Object.freeze({ TASK: "task", PROJECT: "project" });

export const SCOPES = Object.freeze(Object.values(Scope));

/** Most recently updated first. Returns a new array. */
export const byUpdated = (list) => [...list].sort((a, b) => b.updatedAt.getTime() - a.updatedAt.getTime());

/** The list already holds this artifact (an earlier revision of it). */
export const isKnown = (list, artifact) => list.some((a) => a.id === artifact.id);

/** The list after a publish: the artifact replaces its earlier revision and the list stays newest first. */
export function applyPublish(list, artifact) {
  return byUpdated([artifact, ...list.filter((a) => a.id !== artifact.id)]);
}

/**
 * The incoming artifact says something the known one does not: a new
 * revision, or the same revision changed later (a move between scopes).
 */
export function isNewer(known, incoming) {
  if (!known) return true;
  if (incoming.revision !== known.revision) return incoming.revision > known.revision;
  return incoming.updatedAt.getTime() > known.updatedAt.getTime();
}

/** A first publish or a re-publish: what the Design tab counts. A move is not one. */
export function isPublish(list, artifact) {
  const known = list.find((a) => a.id === artifact.id);
  return !known || artifact.revision > known.revision;
}

/** The list after a change that is not a publish: the artifact is replaced where it stands. */
export const applyChange = (list, artifact) => list.map((a) => (a.id === artifact.id ? artifact : a));

export const withoutArtifact = (list, id) => list.filter((a) => a.id !== id);

/** It can move to the project: it has a file to keep. A dev-server url dies with its session. */
export const isPromotable = (artifact) => artifact.kind !== Kind.URL;

const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);

/** http(s) on this machine: the only URLs the Design tab embeds (the server validates too; both halves hold). */
export function isLoopbackUrl(url) {
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    return false;
  }
  return (parsed.protocol === "http:" || parsed.protocol === "https:") && LOOPBACK_HOSTS.has(parsed.hostname);
}

/** The last segment of the artifact's path; "" for a url. */
export const fileName = (artifact) => artifact.path.split("/").filter(Boolean).at(-1) ?? "";
