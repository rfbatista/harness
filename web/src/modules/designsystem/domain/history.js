// The History tab's model: the version list's rows in words, and how pages of
// the list join. Pure.

import { relativeTime } from "../../../shared/presentation/format.js";

/** How many versions one request asks for. */
export const VERSIONS_PAGE = 30;

const SOURCES = { manual: "Edited by hand", import: "Imported from a repository", proposal: "Agent proposal" };

/** A version's source in words; one this page does not know is shown as it is. */
export const sourceText = (source) => SOURCES[source] ?? source;

const PARTS = [
  ["tokens", "tokens"],
  ["principles", "principles"],
  ["components", "components"],
];

/** A "+added ~changed -removed" count without its zero terms ("+0 ~1 -0" is "~1"). */
function countsText(counts) {
  return String(counts ?? "")
    .split(" ")
    .filter((t) => t !== "" && !/^[+~-]0$/.test(t))
    .join(" ");
}

/** What a version changed, per part, leaving out the parts it left alone. */
export function summaryText(summary) {
  const parts = PARTS.map(([key, label]) => [label, countsText(summary?.[key])]).filter(([, c]) => c !== "");
  return parts.length === 0 ? "no change" : parts.map(([label, c]) => `${label} ${c}`).join(" · ");
}

/** One row of the list: who, when, source, message and summary, as text. */
export function versionRow(v, now) {
  const then = new Date(v.at);
  const ago = Number.isNaN(then.getTime()) ? "" : relativeTime(then, now);
  return {
    version: v.version,
    label: `Version ${v.version}`,
    by: v.by === "person" ? "you" : v.by,
    when: ago === "" ? "" : ago === "now" ? "just now" : `${ago} ago`,
    source: sourceText(v.source),
    message: v.message,
    hasMessage: v.message !== "",
    summary: summaryText(v.summary),
  };
}

/** A fresh first page over the list held: its versions replace the head, the older ones already loaded stay. */
export function mergeVersions(current, fresh) {
  if (fresh.length === 0) return current;
  const oldest = fresh[fresh.length - 1].version;
  return [...fresh, ...current.filter((v) => v.version < oldest)];
}

/** Whether versions older than the last of the list may exist: a full page that does not reach version 1. */
export function hasMore(versions) {
  return versions.length >= VERSIONS_PAGE && versions[versions.length - 1].version > 1;
}
