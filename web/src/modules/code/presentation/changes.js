// The Changes list as the code worker's view model: files grouped by
// directory, the legend with its counts and filter, the Working scope's
// Tracked changes and Untracked sections, and the head text. Pure functions
// of the files read so far and the diff totals, so filtering and regrouping
// read no page. Rows are keyed by path: a file read again, from another page
// or in another section, is one row.

import { baseName, changeMark, dirName, formatCount, plural } from "../domain/code.js";
import { slugs } from "../domain/diff.js";
import { summaryRowView } from "./view.js";

/** The order of the legend's buttons (the mockup's): M A D R C T U, so modified leads. */
const LEGEND_ORDER = Object.freeze(["modified", "added", "deleted", "renamed", "copied", "type_changed", "untracked"]);

/** What each section of the Working scope is called. */
const SECTIONS = Object.freeze([
  { key: "tracked", title: "Tracked changes", note: "vs HEAD" },
  { key: "untracked", title: "Untracked", note: "new files, not added" },
]);

/** What the head of the files at the repository's top level is called. */
const ROOT_LABEL = "./";

const byPath = (a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0);

/**
 * The files of the pages read so far as one list: one entry per path, the
 * latest read of a path winning, in path order.
 * @param {import("../domain/code.js").FileDiff[][]} pages  oldest read first
 * @returns {import("../domain/code.js").FileDiff[]}
 */
export function mergeFiles(pages) {
  const byKey = new Map();
  for (const page of pages) for (const f of page) byKey.set(f.path, f);
  return [...byKey.values()].sort(byPath);
}

/**
 * "12 files · +279 −66 · 3 new files": the whole diff's totals; the new
 * files part only in the Working scope and only when there are some.
 * @param {Pick<import("../domain/code.js").CodeDiff, "fileCount"|"added"|"deleted"|"sections">} totals
 */
export function headText(totals, scope) {
  const parts = [plural(totals.fileCount, "file"), `+${formatCount(totals.added)} −${formatCount(totals.deleted)}`];
  const fresh = scope === "working" ? totals.sections?.untracked ?? 0 : 0;
  if (fresh > 0) parts.push(`${formatCount(fresh)} new ${fresh === 1 ? "file" : "files"}`);
  return parts.join(" · ");
}

/** A button of the legend: a status, its count over the whole diff, and whether it filters. */
function legendOf(counts, filter) {
  return LEGEND_ORDER.filter((s) => (counts?.[s] ?? 0) > 0).map((status) => {
    const mark = changeMark(status);
    const count = counts[status];
    return { status, letter: mark.letter, word: mark.word, count, pressed: filter.has(status), label: `${formatCount(count)} ${mark.word.toLowerCase()}` };
  });
}

const sectionOf = (f) => f.section ?? (f.status === "untracked" ? "untracked" : "tracked");

/** What the server's totals say, or, until it says it, what the files read so far add up to. */
function withTotals(totals, files, scope) {
  let { counts, sections } = totals;
  // The wire mapper writes zeros for counts the server does not send, so all zeros over files that were read is no answer.
  if (!counts || (files.length > 0 && Object.values(counts).every((n) => n === 0))) {
    counts = {};
    for (const f of files) counts[f.status] = (counts[f.status] ?? 0) + 1;
  }
  if (!sections && scope === "working") {
    sections = { tracked: 0, untracked: 0, staged: 0 };
    for (const f of files) {
      sections[sectionOf(f)] += 1;
      if (f.staged) sections.staged += 1;
    }
  }
  return { ...totals, counts, sections };
}

/** A row of the list: the summary row, plus the staged flag and "new" for an untracked file. */
function rowOf(f, slug, current, fresh, href) {
  const base = summaryRowView(f, { slug, current, fresh });
  const isNew = f.status === "untracked";
  const note = f.binary ? "binary" : isNew ? "new" : base.note;
  const flag = f.staged ? "staged" : "";
  const label = `${f.path}, ${base.mark?.word.toLowerCase() ?? f.status}${isNew && !f.binary ? ", new" : ""}${base.stat && !isNew ? `, ${base.stat.added} ${base.stat.deleted}` : ""}${flag ? ", staged" : ""}`;
  return { ...base, href: href(slug), dir: "", name: baseName(f.path), stat: isNew ? null : base.stat, note, staged: f.staged, flag, label };
}

/** Rows grouped by directory, the groups in directory order (the root first). */
function groupsOf(rows) {
  const byDir = new Map();
  for (const r of rows) {
    const dir = dirName(r.path);
    if (!byDir.has(dir)) byDir.set(dir, []);
    byDir.get(dir).push(r);
  }
  return [...byDir.entries()]
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([dir, list]) => ({ key: dir, dir: dir ? `${dir}/` : "", label: dir ? `${dir}/` : ROOT_LABEL, count: list.length, rows: list }));
}

/**
 * The Changes list.
 * @param {object} p
 * @param {import("../domain/code.js").FileDiff[]} p.files  the files read so far, one per path (see mergeFiles)
 * @param {Pick<import("../domain/code.js").CodeDiff, "fileCount"|"added"|"deleted"|"counts"|"sections">} p.totals  from any page of the same diff
 * @param {import("../domain/code.js").DiffScope} p.scope
 * @param {Iterable<string>} p.filter  the statuses pressed in the legend; none pressed shows everything
 * @param {Map<string, string>} [p.slugMap]  the anchors of the diff's file sections; made from the files when absent
 * @param {string} [p.current]  the path of the file on screen
 * @param {Iterable<string>} [p.fresh]  the paths that changed since the reader last looked
 * @param {(slug: string, path: string) => string} [p.href]  where a row links; the diff section's anchor by default (the History tab links into the Code tab)
 * @param {Map<string, object>} [p.cache]  rows kept between calls: a row whose file, slug, current and fresh state are as before is the same object
 */
export function changesList({ files, totals: given, scope, filter, slugMap, current = "", fresh = [], href = (slug) => `#diff-${slug}`, cache }) {
  const pressed = new Set(filter);
  const totals = withTotals(given, files, scope);
  const slug = slugMap ?? slugs(files.map((f) => f.path));
  const freshSet = new Set(fresh);
  const shown = files.filter((f) => pressed.size === 0 || pressed.has(f.status)).sort(byPath);
  const rows = shown.map((f) => {
    const s = slug.get(f.path);
    const isCurrent = f.path === current;
    const isFresh = freshSet.has(f.path);
    const hit = cache?.get(f.path);
    if (hit && hit.f === f && hit.slug === s && hit.current === isCurrent && hit.fresh === isFresh) return { f, row: hit.row };
    const row = rowOf(f, s, isCurrent, isFresh, (slug) => href(slug, f.path));
    cache?.set(f.path, { f, slug: s, current: isCurrent, fresh: isFresh, row });
    return { f, row };
  });
  const section = (key, title, note, list, whole) => {
    const groups = groupsOf(list.map((x) => x.row));
    return { key, title, note, count: pressed.size === 0 && whole !== undefined ? whole : list.length, groups };
  };
  const sections =
    scope === "working"
      ? SECTIONS.map((s) => section(s.key, s.title, s.note, rows.filter((x) => sectionOf(x.f) === s.key), totals.sections?.[s.key])).filter((s) => s.groups.length > 0)
      : rows.length ? [section("files", "", "", rows, totals.fileCount)] : [];
  const legend = legendOf(totals.counts, pressed);
  const showing = LEGEND_ORDER.filter((st) => pressed.has(st)).map((st) => changeMark(st).word.toLowerCase());
  const note = showing.length === 0 ? "" : `· showing ${showing.length === 1 ? showing[0] : `${showing.slice(0, -1).join(", ")} and ${showing.at(-1)}`}`;
  return {
    head: { text: headText(totals, scope), note, files: plural(totals.fileCount, "file"), added: `+${formatCount(totals.added)}`, deleted: `−${formatCount(totals.deleted)}` },
    legend,
    sections,
    empty: sections.length === 0,
  };
}
