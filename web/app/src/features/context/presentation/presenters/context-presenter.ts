// The Context tab: loads one session's context on first show and reads the
// latest entry's facts at a glance. Nothing here changes the session.

import { Presenter } from "../../../../shared/kernel/presenter";
import { loadContext } from "../../application/load-context";
import { ContextSourceError, type ContextSource } from "../../application/ports";
import type { Entry, Facts, SessionContext } from "../../domain/session-context";
import { toolsGroup } from "./tools-group";
import type { ContextGroups, GroupKey } from "./context-view-model";
import { closedPiece, instructionsGroup, type PieceState } from "./instructions-group";

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
  /** The groups below the facts; null until the context is ready. */
  readonly groups: ContextGroups | null;
}

const UNRECORDED = "This session started before the harness recorded its context";

/** Hooks start collapsed; every other group starts open. */
const DEFAULT_OPEN: Record<GroupKey, boolean> = { instructions: true, tools: true, servers: true, skills: true, hooks: false, changes: true };

const idle: ContextViewModel = { status: "idle", message: "", facts: [], groups: null };

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
  /** Groups the person closed (every group starts open but Hooks). */
  readonly #toggled = new Set<GroupKey>();
  readonly #pieces = new Map<string, PieceState>();

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
    this.set({ status: "loading", message: "", facts: [], groups: null });
    try {
      this.#context = await loadContext(this.#source, this.#sessionId);
    } catch (err) {
      if (!(err instanceof ContextSourceError)) throw err;
      if (err.code === "SESSION_NOT_FOUND") this.set({ status: "not_found", message: "This session no longer exists.", facts: [], groups: null });
      else if (err.code === "BAD_RESPONSE") this.set({ status: "error", message: "The harness server sent an unexpected answer for this session's context.", facts: [], groups: null });
      else this.set({ status: "error", message: "The context could not be loaded.", facts: [], groups: null });
      return;
    }
    const latest = this.#context.entries.at(-1);
    if (!this.#context.recorded || !latest) this.set({ status: "unrecorded", message: UNRECORDED, facts: [], groups: null });
    else this.#render();
  }

  /** The entry on show: the latest. */
  #entry(): Entry | undefined {
    return this.#context?.entries.at(-1);
  }

  #isOpen(key: GroupKey): boolean {
    return DEFAULT_OPEN[key] !== this.#toggled.has(key);
  }

  #heading(key: GroupKey, title: string, count: number) {
    return { key, title, count: String(count), open: this.#isOpen(key) };
  }

  #render(): void {
    const entry = this.#entry();
    if (!entry) return;
    this.set({
      status: "ready",
      message: "",
      facts: factRows(entry.facts),
      groups: {
        instructions: instructionsGroup(this.#heading("instructions", "Instructions", entry.instructions.length), entry.instructions, this.#pieces),
        tools: toolsGroup(this.#heading("tools", "Tools", 0), entry.tools, entry.servers, this.#context!.usage),
      },
    });
  }

  /** The person flips a group open or closed. */
  toggleGroup(key: GroupKey): void {
    if (this.#toggled.has(key)) this.#toggled.delete(key);
    else this.#toggled.add(key);
    if (this.viewModel.status === "ready") this.#render();
  }

  /** Opens a group (the ADR 0085 link lands on Instructions); an open one stays open. */
  openGroup(key: GroupKey): void {
    if (!this.#isOpen(key)) this.toggleGroup(key);
  }

  /** A piece's text opens or closes in place. */
  togglePiece(id: string): void {
    const state = this.#pieces.get(id) ?? closedPiece;
    this.#pieces.set(id, { ...state, open: !state.open });
    this.#render();
  }

  /** "Show all": fetches the piece's full text, once. A failure marks that piece only. */
  async showAll(id: string): Promise<void> {
    const entry = this.#entry();
    const state = this.#pieces.get(id) ?? closedPiece;
    if (!entry || state.fullText === "loading" || state.fullText === "done") return;
    this.#pieces.set(id, { ...state, fullText: "loading" });
    this.#render();
    try {
      const full = await this.#source.piece(this.#sessionId, entry.id, id);
      this.#pieces.set(id, { ...(this.#pieces.get(id) ?? closedPiece), fullText: "done", full });
    } catch (err) {
      if (!(err instanceof ContextSourceError)) throw err;
      this.#pieces.set(id, { ...(this.#pieces.get(id) ?? closedPiece), fullText: "failed" });
    }
    this.#render();
  }
}
