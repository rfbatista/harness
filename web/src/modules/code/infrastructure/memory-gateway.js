// An in-memory CodeGateway: repositories and worktrees as plain maps of files,
// read the way the server reads them (contract §1–§10), for tests and
// web/dev. It plays the server too: change() edits a worktree and sends
// code_changed, gone() removes one, baseGone() drops its base branch.
//
// A file is a string (its content), or { binary: size }, { symlink: target },
// or { submodule: true }.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { toDiff, toFile, toMap, toPaths, toSeed, toTree } from "./dto.js";

/**
 * @typedef {string | { binary: number } | { symlink: string } | { submodule: true }} FakeFile
 * @typedef {Record<string, FakeFile>} Files
 * @typedef {{ name: string, head: string, refs: Record<string, { commit: string, files: Files }> }} FakeRepository
 * @typedef {{ repositoryId: string, branch: string, base: string, head: string, committed: Files, disk: Files, untracked: string[], gone?: boolean }} FakeWorkspace
 * @typedef {{ repositoryId: string, parent: string, files: Files }} FakeCommit
 * @typedef {{ contexts: { id: string, name: string, purpose: string }[], zones: { id: string, name: string, pattern: string, contextId: string }[] }} FakeArchitecture
 *
 * @typedef {object} World
 * @property {Record<string, FakeRepository>} repositories
 * @property {Record<string, FakeWorkspace>} [workspaces]
 * @property {Record<string, FakeCommit>} [commits]
 * @property {FakeArchitecture} [architecture]
 * @property {{ perFile?: number, total?: number, path?: number, fileBytes?: number, paths?: number }} [limits]
 * @property {number} [delayMs]
 */

const MAX_FILE = 1 << 20;
const fail = (code, message, status = 400) => {
  throw new StructuredError(code, message, status);
};

const isText = (f) => typeof f === "string";
const kindOf = (f) => (isText(f) || "binary" in f ? "file" : "symlink" in f ? "symlink" : "submodule");
const sizeOf = (f) => (isText(f) ? new TextEncoder().encode(f).length : "binary" in f ? f.binary : null);
const linesOf = (text) => {
  if (!text) return [];
  const l = text.split("\n");
  if (l.at(-1) === "") l.pop();
  return l;
};

/** A glob as zones use it: ** any depth, * within a segment, ? one character. */
export function globMatches(pattern, path) {
  let re = "";
  for (let i = 0; i < pattern.length; i += 1) {
    const c = pattern[i];
    if (c === "*" && pattern[i + 1] === "*") {
      re += pattern[i + 2] === "/" ? "(?:.*/)?" : ".*";
      i += pattern[i + 2] === "/" ? 2 : 1;
    } else if (c === "*") re += "[^/]*";
    else if (c === "?") re += "[^/]";
    else re += c.replace(/[.+^${}()|[\]\\]/g, "\\$&");
  }
  return new RegExp(`^${re}$`).test(path);
}

/** Lines of a minimal diff between two texts (LCS), as context/add/del. */
function lineDiff(a, b) {
  const n = a.length;
  const m = b.length;
  const lcs = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i -= 1) for (let j = m - 1; j >= 0; j -= 1) lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
  const out = [];
  let i = 0;
  let j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && a[i] === b[j]) {
      out.push({ kind: "context", old: i + 1, new: j + 1, text: a[i] });
      i += 1;
      j += 1;
    } else if (j < m && (i >= n || lcs[i][j + 1] >= lcs[i + 1][j])) {
      out.push({ kind: "add", old: null, new: j + 1, text: b[j] });
      j += 1;
    } else {
      out.push({ kind: "del", old: i + 1, new: null, text: a[i] });
      i += 1;
    }
  }
  return out;
}

/** Groups diff lines into hunks with 3 lines of context, in the wire shape. */
function hunksOf(lines, noNewlineAtEnd) {
  const changed = lines.map((l, i) => (l.kind === "context" ? -1 : i)).filter((i) => i >= 0);
  if (changed.length === 0) return [];
  const ranges = [];
  for (const i of changed) {
    const s = Math.max(0, i - 3);
    const e = Math.min(lines.length - 1, i + 3);
    const last = ranges.at(-1);
    if (last && s <= last[1] + 1) last[1] = Math.max(last[1], e);
    else ranges.push([s, e]);
  }
  return ranges.map(([s, e]) => {
    const part = lines.slice(s, e + 1);
    const olds = part.filter((l) => l.kind !== "add");
    const news = part.filter((l) => l.kind !== "del");
    return {
      old_start: olds[0]?.old ?? Math.max(0, (part.find((l) => l.old)?.old ?? 1) - 1),
      old_lines: olds.length,
      new_start: news[0]?.new ?? 0,
      new_lines: news.length,
      header: "",
      lines: part.map((l, k) => ({ kind: l.kind, old: l.old, new: l.new, text: l.text, no_newline: noNewlineAtEnd && s + k === lines.length - 1 })),
    };
  });
}

/**
 * @param {World} world
 */
export function memoryCode(world) {
  const followers = new Set();
  let delayMs = world.delayMs ?? 0;
  let nextFailure = null;
  const calls = [];
  const limits = { perFile: 3000, total: 20_000, path: 20_000, fileBytes: MAX_FILE, paths: 50_000, ...world.limits };
  world.workspaces ??= {};
  world.commits ??= {};
  world.architecture ??= { contexts: [], zones: [] };

  const sorted = (xs) => [...xs].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
  const languageOf = (path) => {
    const ext = path.slice(path.lastIndexOf(".") + 1);
    return { go: "go", js: "javascript", md: "markdown", json: "json", css: "css", html: "html", templ: "templ", ts: "typescript" }[ext] ?? "plaintext";
  };
  const zonesOf = (path) => world.architecture.zones.filter((z) => z.pattern && globMatches(z.pattern, path)).map((z) => z.id);

  async function pause(name, args) {
    calls.push({ name, args });
    if (delayMs) await new Promise((r) => setTimeout(r, delayMs));
    if (nextFailure) {
      const f = nextFailure;
      nextFailure = null;
      fail(f.code, f.message, f.status);
    }
  }

  function repository(id) {
    const repo = world.repositories[id];
    if (!repo) fail(Codes.REPOSITORY_NOT_FOUND, "no such repository", 404);
    return repo;
  }

  function workspace(id, { needDisk = true } = {}) {
    const ws = world.workspaces[id];
    if (!ws) fail(Codes.WORKSPACE_NOT_FOUND, "no such workspace", 404);
    if (needDisk && ws.gone) fail(Codes.WORKSPACE_MISSING, "the session's worktree is gone", 409);
    return ws;
  }

  /** A source's files, its echo, and (for a workspace) its base files. */
  function open(src) {
    const hasRepo = !!src.repositoryId;
    const hasWs = !!src.workspaceId;
    if (hasRepo === hasWs) fail(Codes.INVALID_INPUT, "give exactly one of repository_id and workspace_id");
    if (hasWs) {
      if (src.ref) fail(Codes.INVALID_INPUT, "ref is not allowed with workspace_id");
      const ws = workspace(src.workspaceId);
      const repo = repository(ws.repositoryId);
      const base = repo.refs[ws.base];
      return {
        files: ws.disk,
        // Deleted against the base, as tree changes are (scope all), whether committed or not.
        deleted: [...new Set([...Object.keys(ws.committed), ...Object.keys(base?.files ?? {})])].filter((p) => !(p in ws.disk)),
        base: base?.files ?? null,
        ws,
        source: { kind: "workspace", repository_id: ws.repositoryId, workspace_id: src.workspaceId, ref: ws.branch, commit: ws.head, gone: false },
      };
    }
    const repo = repository(src.repositoryId);
    const ref = src.ref || repo.head;
    if (/[~^:@]|\.\./.test(ref)) fail(Codes.INVALID_INPUT, `${ref} is not a ref`);
    const at = repo.refs[ref] ?? Object.values(repo.refs).find((r) => r.commit.startsWith(ref) && ref.length >= 4) ?? commitFiles(src.repositoryId, ref);
    if (!at) fail(Codes.REF_NOT_FOUND, `${ref} does not resolve`, 404);
    return { files: at.files, deleted: [], base: null, ws: null, source: { kind: "ref", repository_id: src.repositoryId, workspace_id: null, ref, commit: at.commit, gone: false } };
  }

  function commitFiles(repositoryId, hash) {
    const entry = Object.entries(world.commits).find(([h, c]) => c.repositoryId === repositoryId && hash.length >= 4 && h.startsWith(hash));
    return entry ? { commit: entry[0], files: entry[1].files } : null;
  }

  function changeOf(o, path) {
    if (!o.ws || !o.base) return null;
    if (o.ws.untracked.includes(path) && path in o.files) return "untracked";
    if (!(path in o.files)) return "deleted";
    if (!(path in o.base)) return "added";
    return JSON.stringify(o.base[path]) === JSON.stringify(o.files[path]) ? null : "modified";
  }

  function checkPath(path) {
    if (path.startsWith("/") || path.split("/").some((s) => s === ".." || s === "") || path.includes("\0")) fail(Codes.INVALID_INPUT, `unsafe path ${path}`);
  }

  function tree(src, dir) {
    if (dir) checkPath(dir);
    const o = open(src);
    const all = [...Object.keys(o.files), ...o.deleted];
    const prefix = dir ? `${dir}/` : "";
    if (dir && !all.some((p) => p.startsWith(prefix))) {
      if (dir in o.files) fail(Codes.PATH_NOT_FOUND, `${dir} is not a directory`, 404);
      fail(Codes.PATH_NOT_FOUND, `${dir} is not in the source`, 404);
    }
    const children = new Map();
    for (const p of all) {
      if (!p.startsWith(prefix)) continue;
      const rest = p.slice(prefix.length);
      const slash = rest.indexOf("/");
      if (slash >= 0) {
        const path = prefix + rest.slice(0, slash);
        const prev = children.get(path);
        const ch = changeOf(o, p);
        children.set(path, { path, name: rest.slice(0, slash), kind: "dir", size: null, target: null, change: prev?.change ?? (ch ? "modified" : null), zones: zonesOf(path) });
      } else {
        const f = o.files[p];
        const deleted = !(p in o.files);
        children.set(p, {
          path: p,
          name: rest,
          kind: deleted ? "file" : kindOf(f),
          size: deleted ? null : sizeOf(f),
          target: !deleted && !isText(f) && "symlink" in f ? f.symlink : null,
          change: changeOf(o, p),
          zones: zonesOf(p),
        });
      }
    }
    const entries = [...children.values()].sort((a, b) => (a.kind === "dir") !== (b.kind === "dir") ? (a.kind === "dir" ? -1 : 1) : a.name < b.name ? -1 : 1);
    return { source: o.source, path: dir, entries };
  }

  function file(src, path) {
    checkPath(path);
    const o = open(src);
    const f = o.files[path];
    if (f === undefined || !isText(f) && !("binary" in f)) fail(Codes.PATH_NOT_FOUND, `${path} is not a file here`, 404);
    if (!isText(f)) return { source: o.source, path, size: f.binary, binary: true, truncated: false, language: languageOf(path), content: null, line_count: 0, zones: zonesOf(path) };
    const size = sizeOf(f);
    let content = f;
    let truncated = false;
    if (size > limits.fileBytes) {
      truncated = true;
      content = f.slice(0, limits.fileBytes);
      content = content.slice(0, content.lastIndexOf("\n") + 1);
    }
    return { source: o.source, path, size, binary: false, truncated, language: languageOf(path), content, line_count: linesOf(content).length, zones: zonesOf(path) };
  }

  /** The changed files from files a to files b, with hunks within the limits. */
  function compare(a, b, { untracked = [], path = "" } = {}) {
    const paths = sorted(new Set([...Object.keys(a), ...Object.keys(b)]));
    const files = [];
    let budget = path ? limits.path : limits.total;
    let truncated = false;
    for (const p of paths) {
      if (path && p !== path) continue;
      const x = a[p];
      const y = b[p];
      if (JSON.stringify(x) === JSON.stringify(y)) continue;
      const status = x === undefined ? (untracked.includes(p) ? "untracked" : "added") : y === undefined ? "deleted" : "modified";
      const binary = (x !== undefined && !isText(x)) || (y !== undefined && !isText(y));
      const fd = { path: p, old_path: null, status, binary, added: -1, deleted: -1, hunks: null, withheld: null, language: languageOf(p), zones: zonesOf(p) };
      if (binary) {
        fd.withheld = "binary";
      } else {
        const lines = lineDiff(linesOf(x ?? ""), linesOf(y ?? ""));
        fd.added = lines.filter((l) => l.kind === "add").length;
        fd.deleted = lines.filter((l) => l.kind === "del").length;
        const n = fd.added + fd.deleted;
        if ((!path && n > limits.perFile) || n > budget) {
          fd.withheld = "too_large";
          if (!path && n <= limits.perFile) truncated = true;
        } else {
          budget -= n;
          fd.hunks = hunksOf(lines, typeof y === "string" && y !== "" && !y.endsWith("\n"));
        }
      }
      files.push(fd);
    }
    if (path && files.length === 0) fail(Codes.PATH_NOT_FOUND, `${path} did not change`, 404);
    const sum = (k) => files.filter((f) => !f.binary).reduce((s, f) => s + Math.max(0, f[k]), 0);
    return { files, added: sum("added"), deleted: sum("deleted"), truncated };
  }

  function diff(q) {
    if (q.workspaceId) {
      const scope = q.scope || "all";
      if (!["all", "branch", "working"].includes(scope)) fail(Codes.INVALID_INPUT, `unknown scope ${scope}`);
      const ws = workspace(q.workspaceId, { needDisk: scope !== "branch" });
      const repo = repository(ws.repositoryId);
      const base = repo.refs[ws.base];
      if (scope !== "working" && !base) fail(Codes.REF_NOT_FOUND, `the base ${ws.base} no longer resolves`, 404);
      const [a, b] = scope === "all" ? [base.files, ws.disk] : scope === "branch" ? [base.files, ws.committed] : [ws.committed, ws.disk];
      const untracked = scope === "branch" ? [] : ws.untracked;
      const out = compare(a, b, { untracked, path: q.path });
      return {
        source: { kind: "workspace", repository_id: ws.repositoryId, workspace_id: q.workspaceId, ref: ws.branch, commit: ws.head, gone: !!ws.gone },
        base: scope === "working" ? ws.head : base.commit,
        scope,
        ...out,
      };
    }
    const repo = repository(q.repositoryId);
    if (q.commit) {
      const c = world.commits[q.commit] ?? Object.entries(world.commits).find(([h]) => h.startsWith(q.commit))?.[1];
      if (!c || c.repositoryId !== q.repositoryId) fail(Codes.COMMIT_NOT_FOUND, `no commit ${q.commit}`, 404);
      const parent = c.parent ? world.commits[c.parent]?.files ?? {} : {};
      const hash = q.commit.length === 40 ? q.commit : Object.keys(world.commits).find((h) => h.startsWith(q.commit));
      return { source: { kind: "ref", repository_id: q.repositoryId, workspace_id: null, ref: hash, commit: hash, gone: false }, base: c.parent || "", scope: "commit", ...compare(parent, c.files, { path: q.path }) };
    }
    const base = repo.refs[q.base];
    const head = repo.refs[q.head];
    if (!base || !head) fail(Codes.REF_NOT_FOUND, "base or head does not resolve", 404);
    return { source: { kind: "ref", repository_id: q.repositoryId, workspace_id: null, ref: q.head, commit: head.commit, gone: false }, base: base.commit, scope: "range", ...compare(base.files, head.files, { path: q.path }) };
  }

  function paths(src) {
    const o = open(src);
    const all = sorted(Object.keys(o.files).filter((p) => kindOf(o.files[p]) === "file"));
    return { source: o.source, paths: all.slice(0, limits.paths), truncated: all.length > limits.paths };
  }

  function map(src) {
    const o = open(src);
    const files = Object.keys(o.files).filter((p) => kindOf(o.files[p]) === "file");
    const count = (z) => files.filter((p) => z.pattern && globMatches(z.pattern, p)).length;
    const zc = (z) => ({ id: z.id, name: z.name, pattern: z.pattern, file_count: count(z) });
    const byName = (a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : a.id < b.id ? -1 : 1);
    const { contexts, zones } = world.architecture;
    const zoned = new Set(files.filter((p) => zonesOf(p).length));
    return {
      source: o.source,
      contexts: [...contexts].sort(byName).map((c) => {
        const own = zones.filter((z) => z.contextId === c.id).sort(byName);
        return { id: c.id, name: c.name, purpose: c.purpose, zones: own.map(zc), file_count: files.filter((p) => own.some((z) => z.pattern && globMatches(z.pattern, p))).length };
      }),
      unassigned_zones: zones.filter((z) => !contexts.some((c) => c.id === z.contextId)).sort(byName).map(zc),
      unzoned_file_count: files.length - zoned.size,
      file_count: files.length,
    };
  }

  // Answers are built in the wire shape and decoded by the same mapper as /api.

  /** @type {import("../domain/ports.js").CodeGateway} */
  const gateway = {
    async tree(src, dir) {
      await pause("tree", { src, dir });
      return toTree(tree(src, dir));
    },
    async paths(src) {
      await pause("paths", { src });
      return toPaths(paths(src));
    },
    async file(src, path) {
      await pause("file", { src, path });
      return toFile(file(src, path));
    },
    async diff(q) {
      await pause("diff", { q });
      return toDiff(diff(q));
    },
    async map(src) {
      await pause("map", { src });
      return toMap(map(src));
    },
    follow(projectId, onCodeChanged, onStatus) {
      const f = { onCodeChanged, onStatus };
      followers.add(f);
      queueMicrotask(() => followers.has(f) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(f);
    },
    decodeSeed: toSeed,
  };

  return {
    gateway,
    world,
    calls,
    /** Answers in the wire shape, for seeds and the stub API. */
    wire: { tree, file, diff, paths, map },
    /** Writes files into a worktree (null deletes) and sends code_changed. */
    change(workspaceId, files = {}, { untracked = [] } = {}) {
      const ws = world.workspaces[workspaceId];
      for (const [p, v] of Object.entries(files)) {
        if (v === null) delete ws.disk[p];
        else ws.disk[p] = v;
      }
      ws.untracked = [...new Set([...ws.untracked, ...untracked])];
      for (const f of followers) f.onCodeChanged({ workspaceId, head: ws.head });
    },
    /** Sends code_changed without touching anything. */
    ping(workspaceId) {
      for (const f of followers) f.onCodeChanged({ workspaceId, head: world.workspaces[workspaceId]?.head ?? "" });
    },
    /** The worktree is removed from disk. */
    gone(workspaceId) {
      world.workspaces[workspaceId].gone = true;
    },
    /** The workspace's base branch is deleted. */
    baseGone(workspaceId) {
      const ws = world.workspaces[workspaceId];
      delete world.repositories[ws.repositoryId].refs[ws.base];
    },
    slow(ms) {
      delayMs = ms;
    },
    failNext(code, message = code, status = 400) {
      nextFailure = { code, message, status };
    },
    drop() {
      for (const f of followers) f.onStatus(FeedStatus.PAUSED);
    },
    reconnect() {
      for (const f of followers) {
        f.onStatus(FeedStatus.RESYNCED);
        f.onStatus(FeedStatus.LIVE);
      }
    },
    get followerCount() {
      return followers.size;
    },
  };
}
