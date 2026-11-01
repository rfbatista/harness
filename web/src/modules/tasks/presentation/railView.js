// The rail as the markup binds it: each group's heading and each task's row.
// The server copy (internal/adapter/in/web/shell/shell.templ) writes the same
// words, so the live copy replaces it without a visible change.

import { count } from "../../../shared/presentation/format.js";
import { linkState } from "../domain/activity.js";
import { matchParts, needsYou, railMeta } from "../domain/rail.js";
import { STATUSES } from "../domain/task.js";
import { taskHref } from "./boardView.js";

/**
 * @typedef {{
 *   projectId: string,
 *   byTask: Record<string, import("../domain/activity.js").Activity>,
 *   currentId: string,
 *   fresh: Set<string>,    arrived or moved over the feed
 *   signal: Set<string>,   just started waiting on the person: the dot's one-shot ring
 *   focusedId: string,     the row with focus peeks out of a collapsed group
 *   query: string,
 *   kids?: Record<string, import("../domain/task.js").Task[]>,  a group's feature tasks' children, by feature id (railGroups)
 *   kidTotals?: Record<string, number>,
 *   open?: (id: string) => boolean,   a feature's children are shown; folded by default
 *   filtering?: boolean,              the filter decides: every feature shows its matching children
 *   isKid?: boolean,                  the row is a child nested under its feature
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
  const kids = ctx.isKid ? [] : (ctx.kids?.[task.id] ?? []);
  const isFeature = kids.length > 0;
  const open = isFeature && (ctx.filtering === true || ctx.open?.(task.id) === true);
  // A folded feature's row covers its children: their live sessions, reviews and waiting.
  const covered = isFeature && !open ? [task, ...kids] : [task];
  const activity = {
    live: covered.reduce((n, t) => n + (ctx.byTask[t.id]?.live ?? 0), 0),
    attention: covered.some((t) => ctx.byTask[t.id]?.attention),
  };
  const reviews = covered.reduce((n, t) => n + t.pendingReviews, 0);
  const { state } = linkState(activity, reviews);
  let meta = railMeta({ ...task, pendingReviews: reviews }, activity);
  // A child has no merge badge on the rail: its status takes the slot.
  if (ctx.isKid && meta.kind === "merge") meta = { kind: "", merge: null };
  const hasReviews = meta.kind === "reviews";
  const isWaiting = meta.kind === "waiting";
  const isLive = meta.kind === "live";
  const { before, match, after } = matchParts(task.title, ctx.query);
  const total = ctx.kidTotals?.[task.id] ?? kids.length;
  const word = total === 1 ? "task" : "tasks";
  return {
    id: task.id,
    href: taskHref(ctx.projectId, task.id),
    title: task.title,
    current: task.id === ctx.currentId ? "page" : false,
    attention: covered.some((t) => needsYou(t, ctx.byTask[t.id])),
    state,
    hasDot: state !== "" && !ctx.isKid,
    isKid: Boolean(ctx.isKid),
    statusKey: ctx.isKid ? task.status : "",
    hasStatusWord: Boolean(ctx.isKid) && meta.kind === "",
    statusWord: STATUSES.find((s) => s.value === task.status)?.label ?? task.status,
    isFeature,
    open,
    openText: open ? "true" : "false",
    kidsId: `rail-kids-${task.id}`,
    kidsCount: String(total),
    twistyLabel: `${open ? "Collapse" : "Expand"} ${task.title}, ${total} ${word}`,
    kids: kids.map((k) => toRow(k, { ...ctx, isKid: true, kids: undefined })),
    hasReviews,
    reviewsText: hasReviews ? count(reviews, "review") : "",
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
  const nested = { ...ctx, kids: g.kids, kidTotals: g.kidTotals, filtering: g.filtering };
  const rows = g.rows.map((t) => toRow(t, nested));
  const more = g.more.map((t) => toRow(t, nested));
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
