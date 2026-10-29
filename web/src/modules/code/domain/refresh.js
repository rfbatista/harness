// What a refresh re-reads and what it redraws (ADR 0048, ADR 0044). On a
// code_changed or a git_state the Changes list follows by itself: it reads the
// loaded pages without hunks, plus the totals, and the hunks on screen stay as
// they are. A file whose hunks on screen are older than that read is "updated"
// (a dot on its row); the notice says how many, and Reload re-reads the open
// files with hunks. Pure rules over the files the two reads hold.

import { plural } from "./code.js";

/** One page of a list read: the most files the server answers (contract §3). */
export const LIST_PAGE_MAX = 200;
/** A list read's page when fewer files are loaded. */
export const LIST_PAGE_MIN = 50;

/**
 * What the stat listing knows of a file's change: whether hunks on screen
 * still say the same. An untracked file's line counts are not in the listing
 * (the server cannot count them without reading the file), so only its
 * presence counts; the staged flag moves no hunk.
 * @param {import("./code.js").FileDiff} f
 */
export function listSignature(f) {
  const counts = f.status === "untracked" ? "" : `${f.added}:${f.deleted}`;
  return [f.status, f.oldPath ?? "", counts, f.binary ? "b" : ""].join("|");
}

/**
 * How a list read compares with the hunks on screen.
 * `updated` are the paths of the list whose hunks on screen are out of date or
 * not on screen at all; `removed` are paths on screen that the list no longer
 * has (committed, reverted, deleted). A path past the end of what one side has
 * read is not compared: the other side simply read further.
 * @param {import("./code.js").FileDiff[]} shown   the files whose hunks are on screen
 * @param {import("./code.js").FileDiff[]} listed  the files of the list read
 * @param {{ shownNext?: boolean, listedNext?: boolean }} [more]  whether either side has pages it did not read
 */
export function staleness(shown, listed, { shownNext = false, listedNext = false } = {}) {
  const onScreen = new Map(shown.map((f) => [f.path, f]));
  const read = new Set(listed.map((f) => f.path));
  const lastShown = shown.at(-1)?.path ?? "";
  const lastListed = listed.at(-1)?.path ?? "";
  const updated = [];
  const removed = [];
  for (const f of listed) {
    const was = onScreen.get(f.path);
    if (was) {
      if (listSignature(was) !== listSignature(f)) updated.push(f.path);
    } else if (!shownNext || f.path <= lastShown) updated.push(f.path);
  }
  for (const f of shown) if (!read.has(f.path) && (!listedNext || f.path <= lastListed)) removed.push(f.path);
  return { updated, removed, count: updated.length + removed.length };
}

/**
 * A read without hunks cannot say a file's hunks were withheld; the file the
 * reader sees as "not loaded" stays so in the list until its hunks are read.
 * @param {import("./code.js").FileDiff[]} listed
 * @param {import("./code.js").FileDiff[]} shown
 */
export function carryWithheld(listed, shown) {
  const held = new Set(shown.filter((f) => f.withheld === "too_large").map((f) => f.path));
  if (held.size === 0) return listed;
  return listed.map((f) => (held.has(f.path) && !f.withheld ? Object.freeze({ ...f, withheld: "too_large" }) : f));
}

/** How many files a list read asks for per page: what is loaded, within the server's pages. */
export const listLimit = (loaded) => Math.min(LIST_PAGE_MAX, Math.max(LIST_PAGE_MIN, loaded));

/**
 * The list read with one more page of the diff in it: the page's files take
 * the place of the same paths, and its totals and cursor are the newest.
 * @param {import("./code.js").CodeDiff} listing
 * @param {import("./code.js").CodeDiff} page
 */
export function withPage(listing, page) {
  const at = new Map(listing.files.map((f, i) => [f.path, i]));
  const files = [...listing.files];
  for (const f of page.files) {
    if (at.has(f.path)) files[at.get(f.path)] = f;
    else files.push(f);
  }
  return Object.freeze({ ...listing, files: Object.freeze(files), next: page.next, fileCount: page.fileCount, added: page.added, deleted: page.deleted, counts: page.counts, sections: page.sections });
}

/**
 * The notice under the diff bar: what changed since the hunks were loaded, or,
 * right after a Reload that found nothing, that they are up to date.
 * @returns {{ state: "" | "current", text: string, reload: boolean } | null}
 */
export function noticeOf({ count, upToDate }) {
  if (count > 0) return { state: "", text: `${plural(count, "file")} changed since loaded`, reload: true };
  if (upToDate) return { state: "current", text: "Up to date", reload: false };
  return null;
}

/** "updated 4 s ago", in a file head after a reload changed it. */
export function updatedWord(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `updated ${s} s ago`;
  if (s < 3600) return `updated ${Math.floor(s / 60)} min ago`;
  return `updated ${Math.floor(s / 3600)} h ago`;
}

/** "12:41": the time of a read, as the stopped source bar says it. */
export function clockTime(date) {
  const two = (n) => String(n).padStart(2, "0");
  return `${two(date.getHours())}:${two(date.getMinutes())}`;
}

/**
 * Whether a Reload found nothing to say: no file was out of date before it, and
 * none of the files it read again came back different.
 */
export const reloadFoundNothing = ({ staleBefore, changed }) => staleBefore === 0 && changed.length === 0;
