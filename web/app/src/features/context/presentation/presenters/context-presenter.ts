// The Context tab: loads one session's context on first show and reads the
// latest entry's facts at a glance. Nothing here changes the session.

import { Presenter } from "../../../../shared/kernel/presenter";
import { loadContext } from "../../application/load-context";
import { ContextSourceError, type Clipboard, type ContextFeed, type ContextSource } from "../../application/ports";
import type { Change, Entry, Facts, SessionContext } from "../../domain/session-context";
import { hooksGroup, skillOrder, skillsGroup } from "./skills-group";
import { serversGroup } from "./servers-group";
import { toolsGroup } from "./tools-group";
import { changesGroup, changeText, clockTime } from "./changes-group";
import { contextLinks } from "./context-links";
import { normalize } from "./search";
import type { ContextGroups, EntryPicker, GroupKey, SearchViewModel, WhiteboardLink, ContextLink } from "./context-view-model";
import { closedPiece, instructionsGroup, type PieceState } from "./instructions-group";

export type ContextStatus = "idle" | "loading" | "ready" | "unrecorded" | "not_found" | "error";

export interface LiveLine {
  readonly text: string;
  readonly time: string;
}

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
  /** Present when the session has more than one entry (it was resumed). */
  readonly picker: EntryPicker | null;
  /** Present for a delegate when the whiteboard's address is known; hidden otherwise. */
  readonly whiteboard: WhiteboardLink | null;
  /** Where to change what shaped this context; a fix reaches the next session. Targets with no page are left out. */
  readonly links: readonly ContextLink[];
  /** A group a link asked for: the view brings it into sight. Rises with each ask. */
  readonly reveal: { readonly group: GroupKey; readonly request: number } | null;
  /** The session has ended: nothing here changes any more. */
  readonly ended: boolean;
  /** The one line at the top that names the latest change; null when none is on show. */
  readonly live: LiveLine | null;
  /** The groups below the facts; null until the context is ready. */
  readonly groups: ContextGroups | null;
}

const UNRECORDED = "This session started before the harness recorded its context";

/** Hooks start collapsed; every other group starts open. */
const DEFAULT_OPEN: Record<GroupKey, boolean> = { instructions: true, tools: true, servers: true, skills: true, hooks: false, changes: true };

const NO_SEARCH: SearchViewModel = { query: "", focusRequest: 0 };

export interface ContextDeps {
  readonly clipboard: Clipboard;
  /** With a project, the tab follows the project feed while the session runs. */
  readonly feed?: ContextFeed;
  readonly projectId?: string;
  /** The session is running. Default true. */
  readonly live?: boolean;
  /** The time zone times are shown in; the person's own by default. */
  readonly timeZone?: string;
  /** Where the task page shows the agents whiteboard with this session selected. Unset until the whiteboard exists: the link stays hidden. */
  readonly whiteboardHref?: string;
  /** The agent of the session that started this one; "" when a person did. */
  readonly parent?: string;
}

const idle = (ended: boolean): ContextViewModel => ({ status: "idle", message: "", facts: [], groups: null, search: NO_SEARCH, picker: null, whiteboard: null, links: [], reveal: null, ended, live: null });

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
  /** The entry the person picked; null follows the latest. */
  #pickedEntry: string | null = null;
  #focusRequest = 0;
  /** Each entry's skill order, fixed the first time it is shown. */
  readonly #skillOrders = new Map<string, string[]>();

  readonly #clipboard: Clipboard;
  readonly #feed: ContextFeed | undefined;
  readonly #projectId: string;
  readonly #timeZone: string | undefined;
  readonly #whiteboardHref: string | undefined;
  readonly #parent: string;
  #reveal: { group: GroupKey; request: number } | null = null;
  readonly #running: boolean;
  #unfollow: (() => void) | null = null;
  #refreshing = false;
  #again = false;
  /** Rows that changed since the last change was dismissed: "tool:Bash", "skill:x", "server:y". */
  #rings: ReadonlySet<string> = new Set();
  #liveLine: LiveLine | null = null;

  constructor(source: ContextSource, sessionId: string, { clipboard, feed, projectId = "", live = true, timeZone, whiteboardHref, parent = "" }: ContextDeps) {
    super(idle(!live));
    this.#running = live;
    this.#feed = feed;
    this.#projectId = projectId;
    this.#timeZone = timeZone;
    this.#whiteboardHref = whiteboardHref;
    this.#parent = parent;
    this.#clipboard = clipboard;
    this.#source = source;
    this.#sessionId = sessionId;
  }

  /** The tab is shown: loads the context the first time only. */
  open(): void {
    if (this.#started) return;
    this.#started = true;
    void this.#load().then(() => this.#follow());
  }

  /** Stops following the project feed. */
  close(): void {
    this.#unfollow?.();
    this.#unfollow = null;
  }

  #follow(): void {
    if (!this.#running || !this.#feed || this.#unfollow || this.viewModel.status !== "ready") return;
    this.#unfollow = this.#feed.follow(this.#projectId, (change) => {
      if (change.sessionId === this.#sessionId) void this.refresh();
    });
  }

  #show(status: ContextStatus, message: string): void {
    this.set({ status, message, facts: [], groups: null, search: NO_SEARCH, picker: null, whiteboard: null, links: [], reveal: null, ended: !this.#running, live: null });
  }

  /** Loads again after a failed load. */
  retry(): void {
    if (this.viewModel.status !== "error") return;
    void this.#load();
  }

  /** Reads the context again without a loading state: what is on screen stays until the new answer replaces it. */
  async refresh(): Promise<void> {
    if (this.viewModel.status !== "ready") return;
    // Announcements that arrive during a fetch coalesce into one more fetch.
    if (this.#refreshing) {
      this.#again = true;
      return;
    }
    this.#refreshing = true;
    try {
      do {
        this.#again = false;
        const before = this.#context;
        let next: SessionContext;
        try {
          next = await loadContext(this.#source, this.#sessionId);
        } catch (err) {
          if (!(err instanceof ContextSourceError)) throw err;
          continue; // what is on screen stays; the next change tries again
        }
        this.#context = next;
        if (before) this.#noteChanges(before, next);
        this.#render();
      } while (this.#again);
    } finally {
      this.#refreshing = false;
    }
  }

  /** What changed between two reads: the rows to ring and the line that names the newest change. */
  #noteChanges(before: SessionContext, next: SessionContext): void {
    const rings = new Set<string>();
    for (const [name, use] of next.usage.tools) if (use.count !== before.usage.tools.get(name)?.count) rings.add(`tool:${name}`);
    for (const [name, use] of next.usage.skills) if (use.count !== before.usage.skills.get(name)?.count) rings.add(`skill:${name}`);
    const statuses = new Map((before.entries.at(-1)?.servers ?? []).map((s) => [s.name, s.status]));
    for (const s of next.entries.at(-1)?.servers ?? []) if (statuses.has(s.name) && statuses.get(s.name) !== s.status) rings.add(`server:${s.name}`);
    const added: Change | undefined = next.changes.length > before.changes.length ? next.changes[0] : undefined;
    if (rings.size === 0 && !added) return;
    this.#rings = rings;
    this.#liveLine = added ? { text: changeText(added), time: clockTime(added.at, this.#timeZone) } : this.#liveLine;
  }

  /** The live line and the rings go: the person has seen them. */
  dismissLive(): void {
    this.#rings = new Set();
    this.#liveLine = null;
    if (this.viewModel.status === "ready") this.#render();
  }

  async #load(): Promise<void> {
    this.#show("loading", "");
    try {
      this.#context = await loadContext(this.#source, this.#sessionId);
    } catch (err) {
      if (!(err instanceof ContextSourceError)) throw err;
      if (err.code === "SESSION_NOT_FOUND") this.#show("not_found", "This session no longer exists.");
      else if (err.code === "BAD_RESPONSE") this.#show("error", "The harness server sent an unexpected answer for this session's context.");
      else this.#show("error", "The context could not be loaded.");
      return;
    }
    const latest = this.#context.entries.at(-1);
    if (!this.#context.recorded || !latest) this.#show("unrecorded", UNRECORDED);
    else this.#render();
  }

  /** The entry on show: the one the person picked, else the latest. */
  #entry(): Entry | undefined {
    const entries = this.#context?.entries ?? [];
    return entries.find((e) => e.id === this.#pickedEntry) ?? entries.at(-1);
  }

  /** The person picks an entry to look at; picking the latest goes back to following it. */
  selectEntry(id: string): void {
    const entries = this.#context?.entries ?? [];
    if (!entries.some((e) => e.id === id)) return;
    this.#pickedEntry = id === entries.at(-1)?.id ? null : id;
    if (this.viewModel.status === "ready") this.#render();
  }

  /** Per-entry piece state: the same piece id can carry another text in another entry. */
  #pieceStates(entry: Entry): Map<string, PieceState> {
    const out = new Map<string, PieceState>();
    for (const piece of entry.instructions) {
      const state = this.#pieces.get(`${entry.id}/${piece.id}`);
      if (state) out.set(piece.id, state);
    }
    return out;
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
      picker: this.#picker(),
      whiteboard: entry.facts.role === "delegate" && this.#whiteboardHref ? { label: this.#parent === "" ? "Delegate · see on the whiteboard" : `Delegate of ${this.#parent} · see on the whiteboard`, href: this.#whiteboardHref } : null,
      links: contextLinks(entry, this.#sessionId, this.#projectId),
      reveal: this.#reveal,
      ended: !this.#running,
      live: this.#liveLine,
      groups: {
        instructions: instructionsGroup(this.#heading("instructions", "Instructions"), entry.instructions, this.#pieceStates(entry), query),
        tools: toolsGroup(this.#heading("tools", "Tools"), entry.tools, entry.servers, this.#context!.usage, query, this.#rings),
        servers: serversGroup(this.#heading("servers", "MCP servers"), entry.servers, this.#copied, query, this.#rings),
        skills: skillsGroup(this.#heading("skills", "Skills"), entry.skills, this.#skillOrderOf(entry), this.#context!.usage, query, this.#rings),
        hooks: hooksGroup(this.#heading("hooks", "Hooks"), entry.hooks, query),
        changes: changesGroup(this.#heading("changes", "Changes while running"), this.#context!.changes, query, this.#timeZone),
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

  #picker(): EntryPicker | null {
    const entries = this.#context?.entries ?? [];
    if (entries.length < 2) return null;
    const label = (e: Entry) => `${e.kind === "start" ? "Started" : "Resumed"} ${clockTime(e.at, this.#timeZone)}`;
    return { options: entries.map((e) => ({ id: e.id, label: label(e) })), selected: this.#entry()!.id };
  }

  /** The person flips a group open or closed. */
  toggleGroup(key: GroupKey): void {
    if (this.#toggled.has(key)) this.#toggled.delete(key);
    else this.#toggled.add(key);
    if (this.viewModel.status === "ready") this.#render();
  }

  /** A link lands on a group: it opens and the view brings it into sight. Before the context has loaded it waits. */
  openAt(key: GroupKey): void {
    this.openGroup(key);
    this.#reveal = { group: key, request: (this.#reveal?.request ?? 0) + 1 };
    if (this.viewModel.status === "ready") this.#render();
  }

  /** Opens a group (the ADR 0085 link lands on Instructions); an open one stays open. */
  openGroup(key: GroupKey): void {
    if (!this.#isOpen(key)) this.toggleGroup(key);
  }

  /** A piece's text opens or closes in place. */
  togglePiece(id: string): void {
    const entry = this.#entry();
    if (!entry) return;
    const key = `${entry.id}/${id}`;
    this.#pieces.set(key, { ...(this.#pieces.get(key) ?? closedPiece), open: !(this.#pieces.get(key) ?? closedPiece).open });
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
    if (!entry) return;
    const key = `${entry.id}/${id}`;
    const state = this.#pieces.get(key) ?? closedPiece;
    if (state.fullText === "loading" || state.fullText === "done") return;
    this.#pieces.set(key, { ...state, fullText: "loading" });
    this.#render();
    try {
      const full = await this.#source.piece(this.#sessionId, entry.id, id);
      this.#pieces.set(key, { ...(this.#pieces.get(key) ?? closedPiece), fullText: "done", full });
    } catch (err) {
      if (!(err instanceof ContextSourceError)) throw err;
      this.#pieces.set(key, { ...(this.#pieces.get(key) ?? closedPiece), fullText: "failed" });
    }
    this.#render();
  }
}
