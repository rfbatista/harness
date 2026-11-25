// The "Daily digest" block of the Settings section: the time, the server's time zone, the next
// run, and what a quiet day and downtime mean.

import type { Clock } from "../../../../shared/kernel/clock";
import { Presenter } from "../../../../shared/kernel/presenter";
import type { DigestTime, DigestTimeState } from "../../application/digest-time";
import { digestsHref } from "../../application/links";
import { nextRun, whenOf } from "../../domain/calendar";

export interface DigestTimeViewModel {
  readonly state: "loading" | "ready" | "error";
  readonly error: string;
  /** "HH:MM", or "" for none: the time input shows it. */
  readonly value: string;
  /** Changes whenever the input has to go back to `value`: a refused change. */
  readonly revision: number;
  /** The zone, and the next run or why there is none: beside the input. */
  readonly note: string;
  readonly hint: string;
  readonly browseHref: string;
  readonly canClear: boolean;
  readonly busy: boolean;
  /** The product owner is off: the block is dimmed, and still editable. */
  readonly dimmed: boolean;
  /** Why the last change was refused. */
  readonly saveError: string;
}

export const HINT = "Once a day it records what changed since the last digest. A quiet day records nothing. A time missed while the harness was down is recorded once it is back, covering the gap.";
const COMPLETE = /^([01]\d|2[0-3]):[0-5]\d$/;

export class DigestTimePresenter extends Presenter<DigestTimeViewModel> {
  readonly #time: DigestTime;
  readonly #projectId: string;
  readonly #clock: Clock;
  readonly #stop: () => void;

  constructor(time: DigestTime, projectId: string, clock: Clock) {
    super(build(time.state, projectId, clock));
    this.#time = time;
    this.#projectId = projectId;
    this.#clock = clock;
    this.#stop = time.subscribe((state) => this.set(build(state, this.#projectId, this.#clock)));
  }

  destroy(): void {
    this.#stop();
  }

  /** The time the person ended on, when the input loses focus or Enter is pressed: a complete time that differs from the saved one is saved; anything else is left. Never a time on the way: typing 10:45 over 09:00 passes through 01:00, and the scheduler would act on it. */
  change = (value: string): Promise<void> => (COMPLETE.test(value) && value !== (this.#time.state.digestTime ?? "") ? this.#time.set(value) : Promise.resolve());

  clear = (): Promise<void> => (this.#time.state.digestTime === null ? Promise.resolve() : this.#time.set(""));

  retry = (): void => this.#time.retry();
}

function build(s: DigestTimeState, projectId: string, clock: Clock): DigestTimeViewModel {
  return {
    state: s.error !== "" && !s.loaded ? "error" : s.loaded ? "ready" : "loading",
    error: s.error,
    value: s.digestTime ?? "",
    revision: s.refusals,
    note: note(s, clock),
    hint: HINT,
    browseHref: digestsHref(projectId),
    canClear: s.digestTime !== null,
    busy: s.busy,
    dimmed: s.enabled === false,
    saveError: s.saveError,
  };
}

function note(s: DigestTimeState, clock: Clock): string {
  const zone = `${s.timeZone}, the server's time zone`;
  if (s.enabled === false) return `${zone} · paused while the product owner is off`;
  if (s.digestTime === null) return `${zone} · no digest is recorded until a time is set`;
  const next = nextRun(new Date(clock.now()), s.digestTime, s.timeZone);
  return next === null ? zone : `${zone} · next: ${whenOf(next, s.timeZone)}`;
}
