// The Context tab: loads one session's context on first show and reads the
// latest entry's facts at a glance. Nothing here changes the session.

import { Presenter } from "../../../../shared/kernel/presenter";
import { loadContext } from "../../application/load-context";
import { ContextSourceError, type Clipboard, type ContextSource } from "../../application/ports";
import type { Entry, Facts, SessionContext } from "../../domain/session-context";
import { hooksGroup, skillOrder, skillsGroup } from "./skills-group";
import { serversGroup } from "./servers-group";
import { toolsGroup } from "./tools-group";
import { normalize } from "./search";
import type { ContextGroups, GroupKey, SearchViewModel } from "./context-view-model";
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
  readonly search: SearchViewModel;
  /** The groups below the facts; null until the context is ready. */
  readonly groups: ContextGroups | null;
}

const UNRECORDED = "This session started before the harness recorded its context";

/** Hooks start collapsed; every other group starts open. */
const DEFAULT_OPEN: Record<GroupKey, boolean> = { instructions: true, tools: true, servers: true, skills: true, hooks: false, changes: true };

const NO_SEARCH: SearchViewModel = { query: "", focusRequest: 0 };

const idle: ContextViewModel = { status: "idle", message: "", facts: [], groups: null, search: NO_SEARCH };

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
  readonly #copied = new Set<string>();
  #query = "";
  #focusRequest = 0;
  /** Each entry's skill order, fixed the first time it is shown. */
  readonly #skillOrders = new Map<string, string[]>();

  readonly #clipboard: Clipboard;

  constructor(source: ContextSource, sessionId: string, { clipboard }: { clipboard: Clipboard }) {
    super(idle);
    this.#clipboard = clipboard;
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

  /** Reads the context again without a loading state: what is on screen stays until the new answer replaces it. */
  async refresh(): Promise<void> {
    if (this.viewModel.status !== "ready") return;
    try {
      this.#context = await loadContext(this.#source, this.#sessionId);
    } catch (err) {
      if (!(err instanceof ContextSourceError)) throw err;
      return; // the next change tries again
    }
    this.#render();
  }

  async #load(): Promise<void> {
    this.set({ status: "loading", message: "", facts: [], groups: null, search: NO_SEARCH });
    try {
      this.#context = await loadContext(this.#source, this.#sessionId);
    } catch (err) {
      if (!(err instanceof ContextSourceError)) throw err;
      if (err.code === "SESSION_NOT_FOUND") this.set({ status: "not_found", message: "This session no longer exists.", facts: [], groups: null, search: NO_SEARCH });
      else if (err.code === "BAD_RESPONSE") this.set({ status: "error", message: "The harness server sent an unexpected answer for this session's context.", facts: [], groups: null, search: NO_SEARCH });
      else this.set({ status: "error", message: "The context could not be loaded.", facts: [], groups: null, search: NO_SEARCH });
      return;
    }
    const latest = this.#context.entries.at(-1);
    if (!this.#context.recorded || !latest) this.set({ status: "unrecorded", message: UNRECORDED, facts: [], groups: null, search: NO_SEARCH });
    else this.#render();
  }

  /** The entry on show: the latest. */
  #entry(): Entry | undefined {
    return this.#context?.entries.at(-1);
  }

  #skillOrderOf(entry: Entry): string[] {
    let order = this.#skillOrders.get(entry.id);
    if (!order) {
      order = skillOrder(entry.skills, this.#context!.usage);
      this.#skillOrders.set(entry.id, order);
    }
    return order;
  }

  #isOpen(key: GroupKey): boolean {
    return DEFAULT_OPEN[key] !== this.#toggled.has(key);
  }

  #heading(key: GroupKey, title: string) {
    return { key, title, open: this.#isOpen(key) };
  }

  #render(): void {
    const entry = this.#entry();
    if (!entry) return;
    const query = normalize(this.#query);
    this.set({
      status: "ready",
      message: "",
      facts: factRows(entry.facts),
      search: { query: this.#query, focusRequest: this.#focusRequest },
      groups: {
        instructions: instructionsGroup(this.#heading("instructions", "Instructions"), entry.instructions, this.#pieces, query),
        tools: toolsGroup(this.#heading("tools", "Tools"), entry.tools, entry.servers, this.#context!.usage, query),
        servers: serversGroup(this.#heading("servers", "MCP servers"), entry.servers, this.#copied, query),
        skills: skillsGroup(this.#heading("skills", "Skills"), entry.skills, this.#skillOrderOf(entry), this.#context!.usage, query),
        hooks: hooksGroup(this.#heading("hooks", "Hooks"), entry.hooks, query),
      },
    });
  }

  /** The person types in the search box. */
  search(query: string): void {
    this.#query = query;
    if (this.viewModel.status === "ready") this.#render();
  }

  /** Esc in the search box. */
  clearSearch(): void {
    this.search("");
  }

  /** "/": asks the view to focus the search box. */
  focusSearch(): void {
    this.#focusRequest += 1;
    if (this.viewModel.status === "ready") this.#render();
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

  /** Copy on a failed server: claude's error goes to the clipboard. */
  async copyError(serverName: string): Promise<void> {
    const server = this.#entry()?.servers.find((s) => s.name === serverName);
    if (!server || server.status !== "failed") return;
    await this.#clipboard.copy(server.statusReason);
    this.#copied.add(serverName);
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
