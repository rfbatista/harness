// The Context tab: loads one session's context on first show and reads the
// latest entry's facts at a glance. Nothing here changes the session.

import { Presenter } from "../../../../shared/kernel/presenter";
import { loadContext } from "../../application/load-context";
import { ContextSourceError, type ContextSource } from "../../application/ports";
import type { Facts, SessionContext } from "../../domain/session-context";

export type ContextStatus = "idle" | "loading" | "ready" | "unrecorded" | "not_found" | "error";

export interface FactRow {
  readonly label: string;
  readonly value: string;
}

export interface ContextViewModel {
  readonly status: ContextStatus;
  /** What to tell the person in the unrecorded, not-found and error states. */
  readonly message: string;
  readonly facts: readonly FactRow[];
}

const UNRECORDED = "This session started before the harness recorded its context";

const idle: ContextViewModel = { status: "idle", message: "", facts: [] };

export function factRows(f: Facts): FactRow[] {
  return [
    { label: "Agent", value: f.agent === "" ? "Plain claude" : f.role === "" ? f.agent : `${f.agent} · ${f.role}` },
    { label: "Mode & model", value: `${f.mode === "" ? "No mode" : f.mode} · ${f.model}` },
    { label: "Session", value: f.headless ? "Headless session" : "Terminal session" },
    { label: "Permissions", value: f.permissionMode },
    { label: "Sandbox & visibility", value: `${f.sandbox} · ${f.repositoryVisibility}` },
    { label: "Worktree & branch", value: `${f.worktree} · ${f.branch}` },
    { label: "Token savers", value: f.tokenSavers.length === 0 ? "none" : f.tokenSavers.join(", ") },
    { label: "Claude version", value: f.claudeVersion === "" ? "unknown" : f.claudeVersion },
    { label: "Approximate size", value: `≈${tokens(f.approxTokens)} tokens` },
  ];
}

function tokens(n: number): string {
  return n < 1000 ? String(n) : `${(n / 1000).toFixed(1).replace(/\.0$/, "")}k`;
}

export class ContextPresenter extends Presenter<ContextViewModel> {
  readonly #source: ContextSource;
  readonly #sessionId: string;
  #started = false;
  #context: SessionContext | null = null;

  constructor(source: ContextSource, sessionId: string) {
    super(idle);
    this.#source = source;
    this.#sessionId = sessionId;
  }

  /** The tab is shown: loads the context the first time only. */
  open(): void {
    if (this.#started) return;
    this.#started = true;
    void this.#load();
  }

  /** Loads again after a failed load. */
  retry(): void {
    if (this.viewModel.status !== "error") return;
    void this.#load();
  }

  async #load(): Promise<void> {
    this.set({ status: "loading", message: "", facts: [] });
    try {
      this.#context = await loadContext(this.#source, this.#sessionId);
    } catch (err) {
      if (!(err instanceof ContextSourceError)) throw err;
      if (err.code === "SESSION_NOT_FOUND") this.set({ status: "not_found", message: "This session no longer exists.", facts: [] });
      else if (err.code === "BAD_RESPONSE") this.set({ status: "error", message: "The harness server sent an unexpected answer for this session's context.", facts: [] });
      else this.set({ status: "error", message: "The context could not be loaded.", facts: [] });
      return;
    }
    const latest = this.#context.entries.at(-1);
    if (!this.#context.recorded || !latest) this.set({ status: "unrecorded", message: UNRECORDED, facts: [] });
    else this.set({ status: "ready", message: "", facts: factRows(latest.facts) });
  }
}
