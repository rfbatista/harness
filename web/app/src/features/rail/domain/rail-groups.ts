// The rail's groups: every status in RAIL_ORDER, Done capped at its latest,
// folded groups, the filter and Needs you. The server draws the first paint by
// the same rules (BuildRail in internal/adapter/in/web/tasks/rail.go).

import type { Activity } from "./activity";
import { RAIL_ORDER, STATUSES, type Task, type TaskStatus } from "./task";

/** How many done tasks show before "Show N more"; the server's DoneCap. */
export const DONE_CAP = 5;

export interface RailGroup {
  readonly key: TaskStatus;
  readonly label: string;
  /** The group's tasks. */
  readonly total: number;
  /** The tasks the filter or Needs you keep (total when neither is on). */
  readonly matched: number;
  readonly empty: boolean;
  readonly collapsed: boolean;
  /** The filter or Needs you decides what shows: no folding, no cap. */
  readonly filtering: boolean;
  /** Shown (a collapsed group's are hidden by the peek rule, in CSS). */
  readonly rows: readonly Task[];
  /** Done's rest, behind "Show N more". */
  readonly more: readonly Task[];
  /** A feature task's children nested under its row, by its id; the filter may leave some out. */
  readonly kids: Readonly<Record<string, readonly Task[]>>;
  /** How many children each feature has, filtered or not. */
  readonly kidTotals: Readonly<Record<string, number>>;
}

const QUIET: Activity = { live: 0, attention: false };

/** A session waits on the person, or a review request does. */
export const needsYou = (task: Task, activity: Activity = QUIET): boolean => activity.attention || task.pendingReviews > 0;

/** A task's children, in list order. */
export const childrenOf = (tasks: readonly Task[], task: Task): Task[] => (task.id ? tasks.filter((t) => t.parentId === task.id) : []);

/** The order a feature lists its children: what waits on the person first, done last. */
const CHILD_ORDER: readonly TaskStatus[] = ["review", "in_progress", "todo", "backlog", "done"];

/** A feature task's children, archived-alone ones included: Review, In progress, Todo, Backlog, Done; the newest move first within a status. */
export const cardChildren = (tasks: readonly Task[], task: Task): Task[] => childrenOf(tasks, task).sort((a, b) => CHILD_ORDER.indexOf(a.status) - CHILD_ORDER.indexOf(b.status) || (b.statusSince || 0) - (a.statusSince || 0));

/** True when the task has no row of its own: an archived task, or a child whose feature is on the list. */
export const isInsideCard = (tasks: readonly Task[], task: Task): boolean => task.archivedAt !== 0 || (task.parentId !== "" && tasks.some((t) => t.id === task.parentId && !t.archivedAt));

const MERGE_STATUSES = new Set<TaskStatus>(["review", "done"]);

const MERGE_WORDS: Record<string, (m: { merged: number; total: number }) => string> = {
  merged: () => "merged",
  partial: ({ merged, total }) => `${merged}/${total} merged`,
  not_merged: () => "not merged",
  unknown: () => "unknown",
};

/** The merge badge of a Review or Done row: its state and the rail's lower-case word; null when there is nothing to show. */
export function mergeBadge(task: Task): { state: string; rail: string } | null {
  const m = task.mergeState;
  if (!m || !MERGE_STATUSES.has(task.status) || !Object.hasOwn(MERGE_WORDS, m.state)) return null;
  return { state: m.state, rail: MERGE_WORDS[m.state]!(m) };
}

export type RailMetaKind = "reviews" | "waiting" | "live" | "merge" | "";

/** A row's one meta item, the first that applies: the reviews badge, "waiting", "N live", then on Review and Done the merge glyph and word; "" when nothing does. */
export function railMeta(task: Task, activity: Activity = QUIET): { kind: RailMetaKind; merge: { state: string; rail: string } | null } {
  if (task.pendingReviews > 0) return { kind: "reviews", merge: null };
  if (activity.attention) return { kind: "waiting", merge: null };
  if (activity.live > 0) return { kind: "live", merge: null };
  const merge = mergeBadge(task);
  return { kind: merge ? "merge" : "", merge };
}

/** Lower case, without accents: how the filter compares. */
export const normalize = (s: string): string => s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();

/** The title around the query's first match, for <mark>; match is "" when there is none. */
export function matchParts(title: string, query: string): { before: string; match: string; after: string } {
  const q = normalize(query.trim());
  const chars = [...title];
  const folded = chars.map((ch) => normalize(ch));
  const at = q ? folded.join("").indexOf(q) : -1;
  if (at < 0) return { before: title, match: "", after: "" };
  // Map the folded offset back to the title's characters.
  let pos = 0;
  let start = 0;
  while (start < chars.length && pos < at) pos += folded[start++]!.length;
  let end = start;
  while (end < chars.length && pos < at + q.length) pos += folded[end++]!.length;
  return { before: chars.slice(0, start).join(""), match: chars.slice(start, end).join(""), after: chars.slice(end).join("") };
}

export interface RailGroupOptions {
  readonly byTask?: Readonly<Record<string, Activity>>;
  readonly currentId?: string;
  readonly collapsed?: Iterable<string>;
  readonly query?: string;
  readonly needsYouOnly?: boolean;
}

/** The most recently updated first; ties keep the list's order. */
const byLatest = (list: readonly Task[]): Task[] => [...list].sort((a, b) => (b.updatedAt ?? 0) - (a.updatedAt ?? 0));

/** `tasks` in the list's (creation) order. */
export function railGroups(tasks: readonly Task[], { byTask = {}, currentId = "", collapsed = [], query = "", needsYouOnly = false }: RailGroupOptions = {}): RailGroup[] {
  const folded = new Set(collapsed);
  const q = normalize(query.trim());
  const filtering = q !== "" || needsYouOnly;
  const waits = (t: Task): boolean => needsYou(t, byTask[t.id]);
  const matches = (t: Task): boolean => !q || normalize(t.title).includes(q);
  // A feature task's children nest under its row; an archived one is not drawn.
  const allKids = (t: Task): Task[] => cardChildren(tasks, t).filter((k) => !k.archivedAt);
  const waitsRolled = (t: Task): boolean => waits(t) || allKids(t).some(waits);
  // While the filter decides, a feature keeps the children that match, or all of them when it matches itself.
  const kidsOf = (t: Task): Task[] => {
    let kids = allKids(t);
    if (filtering && q && !matches(t)) kids = kids.filter(matches);
    if (filtering && needsYouOnly) kids = kids.filter(waits);
    return kids;
  };
  const keeps = (t: Task): boolean => (matches(t) && (!needsYouOnly || waits(t))) || (t.parentId === "" && filtering && kidsOf(t).length > 0);

  return RAIL_ORDER.map((key) => {
    let list = tasks.filter((t) => t.status === key && !isInsideCard(tasks, t));
    if (key === "done") list = byLatest(list);
    const base = { key, label: STATUSES.find((s) => s.value === key)!.label, total: list.length, empty: list.length === 0, filtering };
    const nest = (rows: readonly Task[]) => ({
      kids: Object.fromEntries(rows.map((t) => [t.id, kidsOf(t)] as const).filter(([, kids]) => kids.length > 0)),
      kidTotals: Object.fromEntries(rows.map((t) => [t.id, allKids(t).length] as const).filter(([, n]) => n > 0)),
    });
    if (filtering) {
      const rows = list.filter(keeps);
      return { ...base, matched: rows.length, collapsed: false, rows, more: [], ...nest(rows) };
    }
    const shown = key === "done" ? list.filter((t, i) => i < DONE_CAP || t.id === currentId || waitsRolled(t)) : list;
    const more = list.filter((t) => !shown.includes(t));
    return { ...base, matched: list.length, collapsed: folded.has(key) && list.length > 0, rows: shown, more, ...nest(list) };
  });
}
