// The rail's groups: every status in RAIL_ORDER, Done capped at its latest,
// folded groups, the filter and Needs you. The BFF draws the first paint by
// the same rules (BuildRail in internal/adapter/in/web/tasks/rail.go), so the
// live copy takes over without a visible change.

import { RAIL_ORDER } from "./board.js";
import { mergeBadge } from "./card.js";
import { cardChildren, isInsideCard } from "./feature.js";
import { STATUSES } from "./task.js";

/** How many done tasks show before "Show N more"; the BFF's DoneCap. */
export const DONE_CAP = 5;

/**
 * @typedef {import("./task.js").Task} Task
 * @typedef {import("./activity.js").Activity} Activity
 * @typedef {{
 *   key: string, label: string,
 *   total: number,        the group's tasks
 *   matched: number,      the tasks the filter or Needs you keep (total when neither is on)
 *   empty: boolean, collapsed: boolean,
 *   filtering: boolean,   the filter or Needs you decides what shows: no folding, no cap
 *   rows: Task[],         shown (a collapsed group's are hidden by the peek rule, in CSS)
 *   more: Task[],         Done's rest, behind "Show N more"
 *   kids: Record<string, Task[]>,       a feature task's children nested under its row, by its id; the filter may leave some out
 *   kidTotals: Record<string, number>,  how many children each feature has, filtered or not
 * }} RailGroup
 */

const QUIET = Object.freeze({ live: 0, attention: false });

/** A session waits on the person, or a review request does. */
export const needsYou = (task, activity = QUIET) => activity.attention || task.pendingReviews > 0;

/**
 * A row's one meta item, the first that applies: the reviews badge,
 * "waiting", "N live", then on Review and Done the merge glyph and word;
 * kind is "" when nothing does. The rail shows no last move or age.
 * @param {Task} task
 * @param {Activity} [activity]
 * @returns {{ kind: "reviews" | "waiting" | "live" | "merge" | "", merge: ReturnType<typeof mergeBadge> }}
 */
export function railMeta(task, activity = QUIET) {
  if (task.pendingReviews > 0) return { kind: "reviews", merge: null };
  if (activity.attention) return { kind: "waiting", merge: null };
  if (activity.live > 0) return { kind: "live", merge: null };
  const merge = mergeBadge(task);
  return { kind: merge ? "merge" : "", merge };
}

/** Lower case, without accents: how the filter compares. */
export const normalize = (s) => s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();

/**
 * The title around the query's first match, for <mark>; match is "" when
 * there is none. Folding accents never changes a title's length in letters,
 * so offsets carry over from the folded string.
 */
export function matchParts(title, query) {
  const q = normalize(query.trim());
  const folded = [...title].map((ch) => normalize(ch));
  const at = q ? folded.join("").indexOf(q) : -1;
  if (at < 0) return { before: title, match: "", after: "" };
  // Map the folded offset back to the title's characters.
  const chars = [...title];
  let pos = 0;
  let start = 0;
  while (start < chars.length && pos < at) pos += folded[start++].length;
  let end = start;
  while (end < chars.length && pos < at + q.length) pos += folded[end++].length;
  return { before: chars.slice(0, start).join(""), match: chars.slice(start, end).join(""), after: chars.slice(end).join("") };
}

/**
 * @param {Task[]} tasks  in the store's (creation) order
 * @param {{
 *   byTask?: Record<string, Activity>,
 *   currentId?: string,
 *   collapsed?: Iterable<string>,
 *   query?: string,
 *   needsYouOnly?: boolean,
 * }} options
 * @returns {RailGroup[]}
 */
export function railGroups(tasks, { byTask = {}, currentId = "", collapsed = [], query = "", needsYouOnly = false }) {
  const folded = new Set(collapsed);
  const q = normalize(query.trim());
  const filtering = q !== "" || needsYouOnly;
  const waits = (t) => needsYou(t, byTask[t.id]);
  const matches = (t) => !q || normalize(t.title).includes(q);
  // A feature task's children nest under its row; an archived one is not drawn.
  const allKids = (t) => cardChildren(tasks, t).filter((k) => !k.archivedAt);
  const waitsRolled = (t) => waits(t) || allKids(t).some(waits);
  // While the filter decides, a feature keeps the children that match, or all of them when it matches itself.
  const kidsOf = (t) => {
    let kids = allKids(t);
    if (filtering && q && !matches(t)) kids = kids.filter(matches);
    if (filtering && needsYouOnly) kids = kids.filter(waits);
    return kids;
  };
  const keeps = (t) => (matches(t) && (!needsYouOnly || waits(t))) || (t.parentId === "" && filtering && kidsOf(t).length > 0);

  return RAIL_ORDER.map((key) => {
    let list = tasks.filter((t) => t.status === key && !isInsideCard(tasks, t));
    if (key === "done") list = byLatest(list);
    const base = { key, label: STATUSES.find((s) => s.value === key).label, total: list.length, empty: list.length === 0, filtering };
    const nest = (rows) => ({
      kids: Object.fromEntries(rows.map((t) => [t.id, kidsOf(t)]).filter(([, kids]) => kids.length > 0)),
      kidTotals: Object.fromEntries(rows.map((t) => [t.id, allKids(t).length]).filter(([, n]) => n > 0)),
    });
    if (filtering) {
      const rows = list.filter(keeps);
      return { ...base, matched: rows.length, collapsed: false, rows, more: [], ...nest(rows) };
    }
    const shown = key === "done" ? list.filter((t, i) => i < DONE_CAP || t.id === currentId || waitsRolled(t)) : list;
    const more = list.filter((t) => !shown.includes(t));
    return {
      ...base,
      matched: list.length,
      collapsed: folded.has(key) && list.length > 0,
      rows: shown,
      more,
      ...nest(list),
    };
  });
}

/** The most recently updated first; ties keep the list's order. */
const byLatest = (list) => [...list].sort((a, b) => (b.updatedAt ?? 0) - (a.updatedAt ?? 0));
