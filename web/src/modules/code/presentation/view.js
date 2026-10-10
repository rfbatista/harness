// What the code views' markup binds, built from the domain: rows of the tree
// and the changed files, file heads, the source bar. The Go first paint
// (internal/adapter/in/web/code/view.go) builds the same views; the shared
// fixtures in web/testdata/views/code keep the two in step.

import { baseName, changeMark, dirMark, dirName, formatCount, formatSize, languageName, plural, shortHash } from "../domain/code.js";
import { notLoaded, slugs, startsCollapsed, statOf, withheldCopy, gitCommand, largestLine } from "../domain/diff.js";
import { digits } from "../domain/lines.js";
import { stateWord } from "../domain/live.js";
import { hrefOf } from "../domain/routes.js";
import { resolveTarget } from "../domain/symlink.js";
import { zoneDot, zoneTags } from "../domain/zones.js";

/**
 * A row of the file tree (file-tree block).
 * @param {import("../domain/tree.js").TreeRow} row
 * @param {{ state: import("../domain/tree.js").TreeState, selected: string, focused: string, zones: Map<string, any>, href: (path: string) => string, fresh: Set<string> }} ctx
 */
export function treeRowView({ entry, depth, parent }, ctx) {
  const siblings = ctx.state.levels[parent] ?? [];
  const isDir = entry.kind === "dir";
  const mark = isDir ? dirMark(entry.change) : entry.change ? changeMark(entry.change) : null;
  const dot = isDir ? null : zoneDot(entry.zones, ctx.zones);
  const word = mark ? `, ${mark.word.toLowerCase()}` : "";
  return {
    key: entry.path,
    path: entry.path,
    name: entry.name,
    kind: entry.kind,
    isDir,
    isLink: entry.kind === "file",
    level: String(depth + 1),
    setsize: String(siblings.length),
    posinset: String(siblings.indexOf(entry) + 1),
    skeleton: false,
    depthStyle: `--depth:${depth}`,
    expanded: isDir ? String(!!ctx.state.expanded[entry.path]) : null,
    busy: isDir && ctx.state.busy[entry.path] ? "true" : null,
    selected: !isDir && ctx.selected === entry.path ? "true" : null,
    tabindex: ctx.focused === entry.path ? "0" : "-1",
    deleted: entry.change === "deleted" ? "deleted" : null,
    fresh: ctx.fresh.has(entry.path) ? "" : null,
    target: entry.kind === "symlink" ? entry.target : "",
    submodule: entry.kind === "submodule",
    href: entry.kind === "file" ? ctx.href(entry.path) : null,
    mark,
    dot,
    label: `${entry.name}${entry.kind === "submodule" ? ", submodule" : entry.kind === "symlink" ? `, link to ${entry.target}` : ""}${word}`,
    title: entry.path,
  };
}

/** The skeleton rows a loading directory shows. */
export const skeletonRows = (dir) => [0, 1, 2].map((i) => ({ key: `${dir}#skeleton${i}` }));

/**
 * A row of the changed files (diff-summary block).
 * @param {import("../domain/code.js").FileDiff} f
 */
export function summaryRowView(f, { slug, current, fresh }) {
  const mark = changeMark(f.status);
  const dir = dirName(f.path);
  const stat = statOf(f);
  return {
    key: f.path,
    path: f.path,
    href: `#diff-${slug}`,
    slug,
    current: current ? "location" : null,
    fresh: fresh ? "" : null,
    mark,
    dir: dir ? `${dir}/` : "",
    name: baseName(f.path),
    stat,
    note: f.binary ? "binary" : notLoaded(f) ? "not loaded" : "",
    old: f.oldPath ? `from ${f.oldPath}` : "",
    label: `${f.path}, ${mark?.word.toLowerCase() ?? f.status}${stat ? `, ${stat.added} ${stat.deleted}` : ""}`,
  };
}

/**
 * A file of the diff (diff-view block), without its lines (drawn by render/lines.js).
 * @param {import("../domain/code.js").FileDiff} f
 */
export function diffFileView(f, { slug, rev, collapsed, busy, asked, zones, diff, viewHref, size, splitAllowed }) {
  const mark = changeMark(f.status);
  const name = baseName(f.path);
  const withheld = withheldCopy(f, { size, asked });
  const canView = f.status !== "deleted" && !f.binary;
  const tags = zoneTags(f.zones, zones);
  return {
    key: `${f.path}@${rev}`,
    path: f.path,
    slug,
    id: `diff-${slug}`,
    nameId: `diff-${slug}-name`,
    bodyId: `diff-${slug}-body`,
    mark,
    oldPath: f.oldPath,
    renamedLabel: f.oldPath ? `${f.oldPath} renamed to ${f.path}` : f.path,
    stat: statOf(f),
    zoneTags: tags.tags,
    zoneMore: tags.more,
    collapsed: collapsed ? "" : null,
    expanded: String(!collapsed),
    toggleLabel: `${collapsed ? "Expand" : "Collapse"} ${name}`,
    busy: busy ? "true" : null,
    hasLines: !!f.hunks && !collapsed,
    withheld,
    command: withheld?.action === "view" ? gitCommand({ scope: diff.scope, base: diff.base, head: diff.source.commit, path: f.path }) : "",
    viewHref: canView ? viewHref : "",
    digitsStyle: `--digits:${digits(largestLine(f.hunks))}`,
    layout: splitAllowed ? "split" : null,
  };
}

/** Each file's slug and whether it starts collapsed. */
export function diffSetup(diff) {
  const s = slugs(diff.files.map((f) => f.path));
  const collapsed = {};
  for (const f of diff.files) if (startsCollapsed(f)) collapsed[f.path] = true;
  return { slugs: s, collapsed };
}

/**
 * The path as links (crumbs block): the repository root, each directory,
 * then the file itself, current.
 */
export function crumbsView(path, { rootName, dirHref }) {
  const parts = path ? path.split("/") : [];
  const crumbs = [{ key: "", label: rootName, href: dirHref(""), current: null }];
  parts.forEach((part, i) => {
    const sub = parts.slice(0, i + 1).join("/");
    const last = i === parts.length - 1;
    crumbs.push({ key: sub, label: part, href: last ? null : dirHref(sub), current: last ? "page" : null });
  });
  return crumbs;
}

/** A file's meta: "Go · 212 lines · 6.1 KB". */
export function fileMeta(file, { highlighted }) {
  const parts = [languageName(file.language)];
  if (!file.binary) parts.push(plural(file.lineCount, "line"));
  parts.push(formatSize(file.size));
  if (!highlighted && !file.binary && file.lineCount > 0) parts.push("plain text: too long to highlight");
  return parts.join(" · ");
}

/** The truncated file's notice (UX §5). */
export function truncatedNotice(file, totalLines) {
  if (!file.truncated) return null;
  const of = totalLines ? ` of ${formatCount(totalLines)}` : "";
  return {
    title: "Showing the first 1 MiB",
    text: `${formatCount(file.lineCount)}${of} lines. The rest of this ${formatSize(file.size)} file is not loaded.`,
    path: file.path,
  };
}

/**
 * The code view's placeholder in place of lines (UX §5), or null when the file has lines.
 * @returns {{ kind: string, title: string, text: string, code: string, action: string, actionHref: string } | null}
 */
export function placeholderOf({ file = null, entry = null, href = (p) => p }) {
  if (entry?.kind === "symlink") {
    const to = resolveTarget(entry.path, entry.target);
    return {
      kind: "symlink",
      title: "Symbolic link",
      text: to ? `points to ${entry.target}` : "Points outside this repository, so it is not shown.",
      code: to ? "" : entry.target,
      action: to ? "Open the target" : "",
      actionHref: to ? href(to) : "",
    };
  }
  if (entry?.kind === "submodule") {
    return { kind: "submodule", title: "Submodule, not read into", text: `${entry.path} is another repository.`, code: "", action: "", actionHref: "" };
  }
  if (!file) return null;
  if (file.binary) return { kind: "binary", title: "Binary file, not shown", text: formatSize(file.size), code: "", action: "", actionHref: "" };
  if (!file.content) return { kind: "empty", title: "Empty file", text: `${baseName(file.path)} has no content.`, code: "", action: "", actionHref: "" };
  return null;
}

/** The source bar's origin and state (code-source-bar block). */
export function sourceBarView({ repositoryName, ref, commit, dirty, state, kind }) {
  return {
    repo: repositoryName,
    branch: ref,
    commit: shortHash(commit),
    commitTitle: commit,
    dirty: kind === "session" && dirty > 0 ? `${formatCount(dirty)} uncommitted` : "",
    state: state || null,
    word: stateWord(state),
  };
}

/** The views switch: Changes (a session only), Files, Map. */
export function viewsView(route, { count, kind, current }) {
  const views = kind === "session" ? ["changes", "files", "map"] : ["files", "map"];
  const labels = { changes: "Changes", files: "Files", map: "Map" };
  return views.map((v) => ({
    key: v,
    label: labels[v],
    href: hrefOf({ ...route, view: v, path: v === "files" ? route.path : "", scope: v === "changes" ? route.scope : "" }),
    current: v === current ? "page" : null,
    count: v === "changes" && count !== null ? String(count) : "",
  }));
}

/** The scope switch: All, Branch, Working, with the ones that cannot answer disabled and titled why. */
export function scopesView(route, { scope, gone, baseGone, baseRef }) {
  const base = baseRef ? `The base branch ${baseRef} no longer exists` : "The base branch no longer exists";
  const why = {
    all: gone ? "The worktree was removed, so its uncommitted work is gone" : baseGone ? base : "",
    branch: baseGone ? base : "",
    working: gone ? "The worktree was removed, so its uncommitted work is gone" : "",
  };
  return ["all", "branch", "working"].map((s) => ({
    key: s,
    label: s[0].toUpperCase() + s.slice(1),
    href: hrefOf({ ...route, view: "changes", path: "", scope: s }),
    current: s === scope ? "page" : null,
    disabled: why[s] ? "true" : null,
    title: why[s] || null,
  }));
}

/** The changed files' head: "12 files +214 −38". */
export function totalsView(diff) {
  return { files: plural(diff.files.length, "file"), added: `+${formatCount(diff.added)}`, deleted: `−${formatCount(diff.deleted)}` };
}
