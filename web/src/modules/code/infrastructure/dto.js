// The wire format of the code browsing contract (/api/code/tree, paths, file,
// diff, map; the project feed's code_changed; the code pages' seeds) and its
// mapping to the domain. Snake case in, frozen camel case out; anything the
// contract does not allow is a BAD_RESPONSE.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { ENTRY_KINDS, SCOPES, STATUSES } from "../domain/code.js";
import { route } from "../domain/routes.js";

const bad = (msg) => {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected code data: ${msg}.`);
};
const str = (v) => (typeof v === "string" ? v : "");
const int = (v, fallback = 0) => (Number.isInteger(v) ? v : fallback);
const strings = (v) => Object.freeze(Array.isArray(v) ? v.filter((s) => typeof s === "string") : []);

/** @returns {import("../domain/code.js").CodeSource} */
export function toSource(dto) {
  if (!dto || (dto.kind !== "ref" && dto.kind !== "workspace")) bad(`source kind "${dto?.kind}"`);
  return Object.freeze({
    kind: dto.kind,
    repositoryId: str(dto.repository_id),
    workspaceId: str(dto.workspace_id),
    ref: str(dto.ref),
    commit: str(dto.commit),
    gone: dto.gone === true,
  });
}

/** @returns {import("../domain/code.js").TreeEntry} */
export function toEntry(dto) {
  if (!dto || typeof dto.path !== "string") bad("tree entry without a path");
  if (!ENTRY_KINDS.includes(dto.kind)) bad(`${dto.path}: kind "${dto.kind}"`);
  if (dto.change != null && !STATUSES.includes(dto.change)) bad(`${dto.path}: change "${dto.change}"`);
  return Object.freeze({
    path: dto.path,
    name: str(dto.name) || dto.path.slice(dto.path.lastIndexOf("/") + 1),
    kind: dto.kind,
    size: Number.isFinite(dto.size) ? dto.size : null,
    target: str(dto.target),
    change: dto.change ?? null,
    zones: strings(dto.zones),
  });
}

/** @returns {import("../domain/code.js").CodeTree} */
export function toTree(dto) {
  if (!dto || !Array.isArray(dto.entries)) bad("tree without entries");
  return Object.freeze({ source: toSource(dto.source), path: str(dto.path), entries: Object.freeze(dto.entries.map(toEntry)) });
}

/** @returns {import("../domain/code.js").CodePaths} */
export function toPaths(dto) {
  if (!dto || !Array.isArray(dto.paths)) bad("paths without a list");
  return Object.freeze({ source: toSource(dto.source), paths: strings(dto.paths), truncated: dto.truncated === true });
}

/** @returns {import("../domain/code.js").CodeFile} */
export function toFile(dto) {
  if (!dto || typeof dto.path !== "string") bad("file without a path");
  const binary = dto.binary === true;
  return Object.freeze({
    source: toSource(dto.source),
    path: dto.path,
    size: int(dto.size),
    binary,
    truncated: dto.truncated === true,
    language: str(dto.language) || "plaintext",
    content: binary || typeof dto.content !== "string" ? null : dto.content,
    lineCount: int(dto.line_count),
    zones: strings(dto.zones),
  });
}

const KINDS = ["context", "add", "del"];

function toHunk(dto, path) {
  if (!dto || !Array.isArray(dto.lines)) bad(`${path}: hunk without lines`);
  return Object.freeze({
    oldStart: int(dto.old_start),
    oldLines: int(dto.old_lines),
    newStart: int(dto.new_start),
    newLines: int(dto.new_lines),
    header: str(dto.header),
    lines: Object.freeze(
      dto.lines.map((l) => {
        if (!KINDS.includes(l?.kind)) bad(`${path}: line kind "${l?.kind}"`);
        return Object.freeze({
          kind: l.kind,
          old: Number.isInteger(l.old) ? l.old : null,
          new: Number.isInteger(l.new) ? l.new : null,
          text: str(l.text),
          noNewline: l.no_newline === true,
        });
      }),
    ),
  });
}

/** @returns {import("../domain/code.js").FileDiff} */
export function toFileDiff(dto) {
  if (!dto || typeof dto.path !== "string") bad("file diff without a path");
  if (!STATUSES.includes(dto.status)) bad(`${dto.path}: status "${dto.status}"`);
  if (dto.withheld != null && dto.withheld !== "too_large" && dto.withheld !== "binary") bad(`${dto.path}: withheld "${dto.withheld}"`);
  return Object.freeze({
    path: dto.path,
    oldPath: str(dto.old_path),
    status: dto.status,
    binary: dto.binary === true,
    added: int(dto.added, -1),
    deleted: int(dto.deleted, -1),
    hunks: Array.isArray(dto.hunks) ? Object.freeze(dto.hunks.map((h) => toHunk(h, dto.path))) : null,
    withheld: dto.withheld ?? null,
    language: str(dto.language) || "plaintext",
    zones: strings(dto.zones),
  });
}

/** @returns {import("../domain/code.js").CodeDiff} */
export function toDiff(dto) {
  if (!dto || !Array.isArray(dto.files)) bad("diff without files");
  if (!SCOPES.includes(dto.scope)) bad(`diff scope "${dto.scope}"`);
  return Object.freeze({
    source: toSource(dto.source),
    base: str(dto.base),
    scope: dto.scope,
    files: Object.freeze(dto.files.map(toFileDiff)),
    added: int(dto.added),
    deleted: int(dto.deleted),
    truncated: dto.truncated === true,
  });
}

const toZoneCount = (z) => Object.freeze({ id: str(z?.id), name: str(z?.name), pattern: str(z?.pattern), fileCount: int(z?.file_count) });

/** @returns {import("../domain/code.js").CodeMap} */
export function toMap(dto) {
  if (!dto || !Array.isArray(dto.contexts)) bad("map without contexts");
  return Object.freeze({
    source: toSource(dto.source),
    contexts: Object.freeze(
      dto.contexts.map((c) =>
        Object.freeze({
          id: str(c?.id),
          name: str(c?.name),
          purpose: str(c?.purpose),
          zones: Object.freeze((Array.isArray(c?.zones) ? c.zones : []).map(toZoneCount)),
          fileCount: int(c?.file_count),
        }),
      ),
    ),
    unassignedZones: Object.freeze((Array.isArray(dto.unassigned_zones) ? dto.unassigned_zones : []).map(toZoneCount)),
    unzonedFileCount: int(dto.unzoned_file_count),
    fileCount: int(dto.file_count),
  });
}

/**
 * A project feed message's code_changed, or null for every other message.
 * @returns {import("../domain/code.js").CodeChanged|null}
 */
export function toCodeChanged(msg) {
  const c = msg?.code_changed;
  if (!c) return null;
  if (typeof c.workspace_id !== "string" || c.workspace_id === "") bad("code_changed without a workspace_id");
  return Object.freeze({ workspaceId: c.workspace_id, head: str(c.head) });
}

/** The wire query of a source. */
export function sourceQuery(src) {
  return src.workspaceId ? { workspace_id: src.workspaceId } : { repository_id: src.repositoryId, ref: src.ref };
}

/** The wire query of a diff. */
export function diffQuery(q) {
  if (q.workspaceId) return { workspace_id: q.workspaceId, scope: q.scope, path: q.path };
  return { repository_id: q.repositoryId, commit: q.commit, base: q.base, head: q.head, path: q.path };
}

/**
 * A code page's seed (internal/adapter/in/web/code: Seed).
 * @returns {import("../domain/ports.js").CodeSeed}
 */
export function toSeed(raw) {
  if (!raw || typeof raw !== "object") bad("seed is not an object");
  const r = raw.route ?? {};
  const src = raw.source ?? {};
  return Object.freeze({
    projectId: str(raw.project_id),
    route: Object.freeze(
      route({
        kind: r.kind === "session" ? "session" : "repository",
        projectId: str(r.project_id) || str(raw.project_id),
        taskId: str(r.task_id),
        sessionId: str(r.session_id),
        repositoryId: str(r.repository_id),
        view: ["changes", "files", "map"].includes(r.view) ? r.view : "",
        path: str(r.path),
        scope: str(r.scope),
        ref: str(r.ref),
        workspace: str(r.workspace),
        commit: str(r.commit),
      }),
    ),
    source: Object.freeze({ repositoryId: str(src.repository_id), ref: str(src.ref), workspaceId: str(src.workspace_id) }),
    repositoryName: str(raw.repository_name),
    branch: str(raw.branch),
    baseRef: str(raw.base_ref),
    live: raw.live === true,
    worktrees: Object.freeze((Array.isArray(raw.worktrees) ? raw.worktrees : []).map((w) => Object.freeze({ id: str(w?.id), name: str(w?.name) }))),
    trees: Object.freeze((Array.isArray(raw.trees) ? raw.trees : []).map(toTree)),
    file: raw.file ? toFile(raw.file) : null,
    diff: raw.diff ? toDiff(raw.diff) : null,
    map: raw.map ? toMap(raw.map) : null,
    error: raw.error && typeof raw.error.code === "string" ? Object.freeze({ code: raw.error.code, message: str(raw.error.message) }) : null,
    refs: strings(raw.refs),
    historyHref: str(raw.history_href),
  });
}
