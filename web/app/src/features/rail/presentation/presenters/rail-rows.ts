// The rail as the view binds it: each group's heading and each task's row.
// The server's first paint writes the same words (internal/adapter/in/web/shell),
// so the island takes over without a visible change.

import type { Activity } from "../../domain/activity";
import { matchParts, needsYou, railMeta, type RailGroup } from "../../domain/rail-groups";
import { STATUSES, taskHref, type Task } from "../../domain/task";

export interface RowContext {
  readonly projectId: string;
  readonly byTask: Readonly<Record<string, Activity>>;
  readonly currentId: string;
  /** Arrived or moved over the feed. */
  readonly fresh: ReadonlySet<string>;
  /** Just started waiting on the person: the dot's one-shot ring. */
  readonly signal: ReadonlySet<string>;
  /** The row with focus peeks out of a collapsed group. */
  readonly focusedId: string;
  readonly query: string;
  /** A feature's children are shown; folded by default. */
  readonly open: (id: string) => boolean;
}

interface Nesting {
  readonly kids: Readonly<Record<string, readonly Task[]>>;
  readonly kidTotals: Readonly<Record<string, number>>;
  /** The filter decides: every feature shows its matching children. */
  readonly filtering: boolean;
  /** The row is a child nested under its feature. */
  readonly isKid: boolean;
}

export interface RowViewModel {
  readonly id: string;
  readonly href: string;
  readonly title: string;
  readonly current: boolean;
  readonly attention: boolean;
  /** The dot: "waiting", "running" or "". */
  readonly state: string;
  readonly hasDot: boolean;
  readonly isKid: boolean;
  /** A child's status dot. */
  readonly statusKey: string;
  readonly hasStatusWord: boolean;
  readonly statusWord: string;
  readonly isFeature: boolean;
  readonly open: boolean;
  readonly kidsId: string;
  readonly kidsCount: number;
  readonly twistyLabel: string;
  readonly kids: readonly RowViewModel[];
  readonly hasReviews: boolean;
  readonly reviewsText: string;
  readonly isWaiting: boolean;
  readonly isLive: boolean;
  readonly liveText: string;
  readonly hasMerge: boolean;
  readonly mergeState: string;
  readonly mergeText: string;
  /** The words around the meta that make the link's name read "Fix flaky test, waiting on you". */
  readonly lead: string;
  readonly tail: string;
  readonly before: string;
  readonly match: string;
  readonly after: string;
  readonly hasMatch: boolean;
  readonly fresh: boolean;
  readonly signal: boolean;
  readonly peek: boolean;
}

export interface GroupViewModel {
  readonly key: string;
  readonly label: string;
  readonly headingId: string;
  readonly listId: string;
  readonly empty: boolean;
  /** The heading is a disclosure button unless the group is empty or the filter decides. */
  readonly foldable: boolean;
  readonly expanded: boolean;
  readonly collapsed: boolean;
  readonly countText: string;
  readonly countWords: string;
  readonly rows: readonly RowViewModel[];
  readonly more: readonly RowViewModel[];
  readonly hasRows: boolean;
  readonly hasMore: boolean;
  readonly moreText: string;
}

const noun = (n: number, word: string): string => `${n} ${word}${n === 1 ? "" : "s"}`;

/** A task's dot: a task or one of its covered children waiting beats running. */
function linkState(activity: Activity, pendingReviews: number): "waiting" | "running" | "" {
  if (activity.attention || pendingReviews > 0) return "waiting";
  if (activity.live > 0) return "running";
  return "";
}

/**
 * A task's row: its dot, one meta item (reviews badge > "waiting" > "N live" >
 * the merge glyph and word) and the hidden words around it.
 */
export function toRow(task: Task, ctx: RowContext, nesting: Nesting): RowViewModel {
  const kids = nesting.isKid ? [] : (nesting.kids[task.id] ?? []);
  const isFeature = kids.length > 0;
  const open = isFeature && (nesting.filtering || ctx.open(task.id));
  // A folded feature's row covers its children: their live sessions, reviews and waiting.
  const covered = isFeature && !open ? [task, ...kids] : [task];
  const activity: Activity = { live: covered.reduce((n, t) => n + (ctx.byTask[t.id]?.live ?? 0), 0), attention: covered.some((t) => ctx.byTask[t.id]?.attention === true) };
  const reviews = covered.reduce((n, t) => n + t.pendingReviews, 0);
  const state = linkState(activity, reviews);
  let meta = railMeta({ ...task, pendingReviews: reviews }, activity);
  // A child has no merge badge on the rail: its status takes the slot.
  if (nesting.isKid && meta.kind === "merge") meta = { kind: "", merge: null };
  const hasReviews = meta.kind === "reviews";
  const isWaiting = meta.kind === "waiting";
  const isLive = meta.kind === "live";
  const { before, match, after } = matchParts(task.title, ctx.query);
  const total = nesting.kidTotals[task.id] ?? kids.length;
  return {
    id: task.id,
    href: taskHref(ctx.projectId, task.id),
    title: task.title,
    current: task.id === ctx.currentId,
    attention: covered.some((t) => needsYou(t, ctx.byTask[t.id])),
    state,
    hasDot: state !== "" && !nesting.isKid,
    isKid: nesting.isKid,
    statusKey: nesting.isKid ? task.status : "",
    hasStatusWord: nesting.isKid && meta.kind === "",
    statusWord: STATUSES.find((s) => s.value === task.status)?.label ?? task.status,
    isFeature,
    open,
    kidsId: `rail-kids-${task.id}`,
    kidsCount: total,
    twistyLabel: `${open ? "Collapse" : "Expand"} ${task.title}, ${total} ${total === 1 ? "task" : "tasks"}`,
    kids: kids.map((k) => toRow(k, ctx, { kids: {}, kidTotals: {}, filtering: nesting.filtering, isKid: true })),
    hasReviews,
    reviewsText: hasReviews ? noun(reviews, "review") : "",
    isWaiting,
    isLive,
    liveText: `${activity.live} live`,
    hasMerge: meta.merge !== null,
    mergeState: meta.merge?.state ?? "",
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

/** A group as the view binds it: its heading, its rows and Done's rest. */
export function toGroup(g: RailGroup, ctx: RowContext): GroupViewModel {
  const nesting: Nesting = { kids: g.kids, kidTotals: g.kidTotals, filtering: g.filtering, isKid: false };
  const rows = g.rows.map((t) => toRow(t, ctx, nesting));
  const more = g.more.map((t) => toRow(t, ctx, nesting));
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
