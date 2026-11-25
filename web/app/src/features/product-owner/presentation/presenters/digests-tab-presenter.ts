// The Digests tab of the product owner's page: the dates in a column, newest first, with a grey
// row for a stretch of quiet days, and the selected digest beside them with its sections.

import type { Clock } from "../../../../shared/kernel/clock";
import { Presenter } from "../../../../shared/kernel/presenter";
import type { ProjectDigests, ProjectDigestsState } from "../../application/project-digests";
import { settingsHref } from "../../application/links";
import { addDays, clockOf, daysBetween, longDay, monthOf, nextRun, shortDay, stampOf, whenOf } from "../../domain/calendar";
import type { Digest, DigestItem } from "../../domain/digest";
import { parseMarkdown, type Block } from "../../domain/markdown";

export interface DigestRowViewModel {
  readonly kind: "digest";
  readonly date: string;
  /** "Mon 23 Nov". */
  readonly label: string;
  /** When its digest time was, "09:00". */
  readonly time: string;
  /** "4 moved · 3 sessions · 1 failed"; "" when there is nothing but what waits on the person. */
  readonly counts: string;
  /** "2 wait on you"; "" for none. */
  readonly attention: string;
  readonly current: boolean;
}

export interface GapRowViewModel {
  readonly kind: "gap";
  /** "Sat 21 – Sun 22", or "Wed 18 Nov" for one day. */
  readonly label: string;
}

export interface MonthRowViewModel {
  readonly kind: "month";
  /** "November 2026". */
  readonly label: string;
}

export type DateRowViewModel = DigestRowViewModel | GapRowViewModel | MonthRowViewModel;

export interface DigestItemViewModel {
  readonly title: string;
  /** "" for an address that is not a page of the web UI: the title is then not a link. */
  readonly href: string;
  /** A task moved from this status; "" for none. */
  readonly from: string;
  readonly to: string;
  /** "finished" or "failed" for a session; "" otherwise. */
  readonly outcome: string;
}

export interface DigestSectionViewModel {
  readonly key: "waiting" | "moved" | "sessions";
  readonly title: string;
  readonly items: readonly DigestItemViewModel[];
}

export interface DigestViewModel {
  readonly date: string;
  /** "Monday 23 November". */
  readonly heading: string;
  /** "Covers Fri 20 Nov 09:00 to Mon 23 Nov 09:00". */
  readonly covers: string;
  /** The summary, parsed from its Markdown. */
  readonly blocks: readonly Block[];
  readonly sections: readonly DigestSectionViewModel[];
}

export interface DigestsTabViewModel {
  /** "empty": no digest to show; the off note and the empty text say why. */
  readonly state: "loading" | "error" | "empty" | "ready";
  readonly error: string;
  /** "Next: Tue 24 Nov, 09:00", or why there is no next run. */
  readonly head: string;
  readonly timeLabel: "Change time" | "Set time";
  readonly timeHref: string;
  /** Said when the product owner is off; "" otherwise. */
  readonly offNote: string;
  readonly turningOn: boolean;
  readonly turnOnError: string;
  readonly emptyText: string;
  readonly rows: readonly DateRowViewModel[];
  readonly gapNote: string;
  /** Earlier digests than the oldest shown exist. */
  readonly hasEarlier: boolean;
  readonly loadingEarlier: boolean;
  /** Why the last page failed; "" otherwise. */
  readonly earlierError: string;
  readonly selected: DigestViewModel | null;
}

export const GAP_NOTE = "Quiet days are not recorded, so the list has gaps.";
const FIRST_COVERS = "The first digest covers the period from when the time was set.";
const KINDS: Record<DigestItem["kind"], DigestSectionViewModel["key"]> = { waiting_on_person: "waiting", task_moved: "moved", session_ended: "sessions", session_failed: "sessions" };
const TITLES: Record<DigestSectionViewModel["key"], string> = { waiting: "Waiting on you", moved: "Tasks that moved", sessions: "Sessions finished or failed" };
const ORDER: readonly DigestSectionViewModel["key"][] = ["waiting", "moved", "sessions"];

const plural = (n: number, one: string, many: string): string => `${n} ${n === 1 ? one : many}`;
/** A link only to a page of this web UI. */
const pageOf = (url: string): string => (url.startsWith("/") && !url.startsWith("//") ? url : "");
/** "in_review" as "In review". */
const status = (value: string | null): string => (value === null || value === "" ? "" : value.charAt(0).toUpperCase() + value.slice(1).replaceAll("_", " "));

export class DigestsTabPresenter extends Presenter<DigestsTabViewModel> {
  readonly #digests: ProjectDigests;
  readonly #projectId: string;
  readonly #clock: Clock;
  readonly #stop: () => void;
  readonly #onSelect: (date: string) => void;
  #selected: string;

  /** `select`: the date the address names, "" for the newest. `onSelect` hears the dates the person chooses. */
  constructor(digests: ProjectDigests, projectId: string, clock: Clock, select = "", onSelect: (date: string) => void = () => {}) {
    super(build(digests.state, projectId, clock, select));
    this.#digests = digests;
    this.#projectId = projectId;
    this.#clock = clock;
    this.#onSelect = onSelect;
    this.#selected = select;
    this.#stop = digests.subscribe((state) => {
      // The digest the person finds first stays the one on screen: a newer one arriving does not take its place.
      if (this.#selected === "" && state.digests[0]) this.#selected = state.digests[0].date;
      this.set(build(state, this.#projectId, this.#clock, this.#selected));
    });
  }

  destroy(): void {
    this.#stop();
  }

  /** A date of the column; one that is not held changes nothing. */
  select = (date: string): void => {
    if (!this.#digests.state.digests.some((d) => d.date === date)) return;
    this.#selected = date;
    this.#onSelect(date);
    this.set(build(this.#digests.state, this.#projectId, this.#clock, date));
  };

  retry = (): void => this.#digests.retry();

  loadEarlier = (): Promise<void> => this.#digests.loadEarlier();

  turnOn = (): Promise<void> => this.#digests.turnOn();
}

function build(s: ProjectDigestsState, projectId: string, clock: Clock, select: string): DigestsTabViewModel {
  const now = new Date(clock.now());
  const held = s.digests;
  const chosen = held.find((d) => d.date === select) ?? held[0] ?? null;
  const off = s.enabled === false;
  const next = !off && s.digestTime !== null ? nextRun(now, s.digestTime, s.timeZone) : null;
  const head = off ? "Paused while the product owner is off" : s.digestTime === null ? "No digest time set" : next === null ? `Digest time ${s.digestTime}` : `Next: ${whenOf(next, s.timeZone)}`;
  const state = s.error !== "" ? "error" : !s.loaded ? "loading" : held.length === 0 ? "empty" : "ready";
  return {
    state,
    error: s.error,
    head,
    timeLabel: s.digestTime === null ? "Set time" : "Change time",
    timeHref: settingsHref(projectId),
    offNote: off ? (held.length === 0 ? "The product owner is off, so no digest is recorded." : `The product owner is off, so no digest is recorded. The ${held.length} digest${held.length === 1 ? "" : "s"} below ${held.length === 1 ? "is" : "are"} kept.`) : "",
    turningOn: s.turningOn,
    turnOnError: s.turnOnError,
    emptyText: emptyText(s, next),
    rows: rows(held, chosen, s.timeZone),
    gapNote: GAP_NOTE,
    hasEarlier: s.hasEarlier,
    loadingEarlier: s.loadingEarlier,
    earlierError: s.earlierError,
    selected: chosen === null ? null : digestOf(chosen, s.timeZone),
  };
}

function emptyText(s: ProjectDigestsState, next: Date | null): string {
  const what = "Tasks that moved, sessions that finished or failed and what waits on you. A quiet day records nothing.";
  if (s.digestTime === null) return `No digest time is set. ${FIRST_COVERS} ${what}`;
  if (s.enabled === false) return `${FIRST_COVERS} ${what}`;
  return `The first digest covers the period from when the time was set, and records ${next === null ? `at ${s.digestTime}` : whenOf(next, s.timeZone)}, if anything changed by then. ${what}`;
}

function rows(held: readonly Digest[], chosen: Digest | null, timeZone: string): DateRowViewModel[] {
  const out: DateRowViewModel[] = [];
  held.forEach((digest, at) => {
    const previous = held[at - 1];
    if (previous && daysBetween(digest.date, previous.date) > 1) out.push({ kind: "gap", label: gapLabel(addDays(digest.date, 1), addDays(previous.date, -1)) });
    if (!previous || monthOf(previous.date) !== monthOf(digest.date)) out.push({ kind: "month", label: monthOf(digest.date) });
    out.push({ kind: "digest", date: digest.date, label: shortDay(digest.date), time: clockOf(digest.periodEnd, timeZone), ...counts(digest), current: digest === chosen });
  });
  return out;
}

const gapLabel = (first: string, last: string): string => (first === last ? shortDay(first) : `${shortDay(first).split(" ").slice(0, 2).join(" ")} – ${shortDay(last).split(" ").slice(0, 2).join(" ")}`);

function counts(digest: Digest): { counts: string; attention: string } {
  const of = (kind: DigestItem["kind"]): number => digest.items.filter((i) => i.kind === kind).length;
  const waiting = of("waiting_on_person");
  const parts = [of("task_moved") > 0 && `${of("task_moved")} moved`, of("session_ended") > 0 && plural(of("session_ended"), "session", "sessions"), of("session_failed") > 0 && `${of("session_failed")} failed`];
  return { counts: parts.filter((p): p is string => p !== false).join(" · "), attention: waiting === 0 ? "" : `${waiting} ${waiting === 1 ? "waits" : "wait"} on you` };
}

function digestOf(digest: Digest, timeZone: string): DigestViewModel {
  const sections = ORDER.map((key) => ({
    key,
    title: TITLES[key],
    items: digest.items.filter((i) => KINDS[i.kind] === key).map((i) => ({ title: i.title, href: pageOf(i.url), from: status(i.from), to: status(i.to), outcome: i.kind === "session_ended" ? "finished" : i.kind === "session_failed" ? "failed" : "" })),
  })).filter((s) => s.items.length > 0);
  return { date: digest.date, heading: longDay(digest.date), covers: `Covers ${stampOf(digest.periodStart, timeZone)} to ${stampOf(digest.periodEnd, timeZone)}`, blocks: parseMarkdown(digest.summary), sections };
}
