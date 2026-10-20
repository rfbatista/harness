// The rail as the markup binds it: each group's heading and each task's row.
// The server copy (internal/adapter/in/web/shell/shell.templ) writes the same
// words, so the live copy replaces it without a visible change.

import { count } from "../../../shared/presentation/format.js";
import { linkState } from "../domain/activity.js";
import { matchParts, needsYou, railMeta } from "../domain/rail.js";
import { taskHref } from "./boardView.js";

const QUIET = Object.freeze({ live: 0, attention: false });

/**
 * @typedef {{
 *   projectId: string,
 *   byTask: Record<string, import("../domain/activity.js").Activity>,
 *   currentId: string,
 *   fresh: Set<string>,    arrived or moved over the feed
 *   signal: Set<string>,   just started waiting on the person: the dot's one-shot ring
 *   focusedId: string,     the row with focus peeks out of a collapsed group
 *   query: string,
 * }} RowContext
 */

/**
 * A task's row: its dot, one meta item (reviews badge > "waiting" > "N live" >
 * the merge glyph and word, railMeta) and the hidden words around it that make the link's name read
 * "Fix flaky test, waiting on you".
 * @param {import("../domain/task.js").Task} task
 * @param {RowContext} ctx
 */
export function toRow(task, ctx) {
  const activity = ctx.byTask[task.id] ?? QUIET;
  const { state } = linkState(activity, task.pendingReviews);
  const meta = railMeta(task, activity);
  const hasReviews = meta.kind === "reviews";
  const isWaiting = meta.kind === "waiting";
  const isLive = meta.kind === "live";
  const { before, match, after } = matchParts(task.title, ctx.query);
  return {
    id: task.id,
    href: taskHref(ctx.projectId, task.id),
    title: task.title,
    current: task.id === ctx.currentId ? "page" : false,
    attention: needsYou(task, activity),
    state,
    hasDot: state !== "",
    hasReviews,
    reviewsText: hasReviews ? count(task.pendingReviews, "review") : "",
    isWaiting,
    isLive,
    liveText: `${activity.live} live`,
    hasMerge: meta.merge !== null,
    mergeState: meta.merge?.state ?? null,
    mergeText: meta.merge?.rail ?? "",
    lead: isLive ? ", running, " : meta.kind ? ", " : "",
    tail: hasReviews ? " waiting on you" : isWaiting ? " on you" : isLive ? (activity.live === 1 ? " session" : " sessions") : "",
    before,
    match,
    after,
    hasMatch: match !== "",
    fresh: ctx.fresh.has(task.id),
    signal: ctx.signal.has(task.id),
    peek: task.id === ctx.focusedId,
  };
}

/**
 * A group as the markup binds it: its heading (a disclosure button unless it
 * is empty or the filter decides), its rows and Done's rest.
 * @param {import("../domain/rail.js").RailGroup} g
 * @param {RowContext} ctx
 */
export function toGroupView(g, ctx) {
  const rows = g.rows.map((t) => toRow(t, ctx));
  const more = g.more.map((t) => toRow(t, ctx));
  const waiting = [...rows, ...more].filter((r) => r.attention).length;
  let countText = String(g.total);
  let countWords = g.total === 1 ? " task" : " tasks";
  if (g.filtering && !g.empty) {
    countText = `${g.matched} of ${g.total}`;
    countWords += " match";
  } else if (waiting > 0) {
    countWords += `, ${waiting} ${waiting === 1 ? "needs" : "need"} you`;
  }
  return {
    key: g.key,
    label: g.label,
    headingId: `rail-${g.key}-heading`,
    listId: `rail-${g.key}`,
    empty: g.empty,
    foldable: !g.empty && !g.filtering,
    expanded: !g.collapsed,
    collapsed: g.collapsed,
    countText,
    countWords,
    rows,
    more,
    hasRows: rows.length + more.length > 0,
    hasMore: more.length > 0,
    moreText: `Show ${more.length} more`,
  };
}
