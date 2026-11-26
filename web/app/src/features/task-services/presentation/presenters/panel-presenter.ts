// The painel de serviços da task: the serviços da task with their live state,
// the task's pending pedidos de serviço, the selo, the falha toast and the
// person's actions. State comes from the server: a first list, then the
// services feed filtered by the serviço da task rule (ADR 0003).

import { Presenter } from "../../../../shared/kernel/presenter";
import type { Clipboard, LiveStatus, PanelPreferences, Scheduler, ServicesEvent, ServicesSource, ServiceVerb } from "../../application/ports";
import { type Decision, type ServiceRequest } from "../../domain/request";
import {
  belongsToTask, firstErrorLine, groupServices, isFalha, neverRan, portLinks, statusView, timeFact, whatItRuns,
  type Dot, type GroupKey, type PortLink, type Service,
} from "../../domain/service";
import { LogPresenter } from "./log-presenter";

/** The page's layout for the panel: a docked column, an overlay on the board's edge (below 64rem), or a bottom sheet (below 48rem). */
export type PanelMode = "docked" | "overlay" | "sheet";

const MASK = "••••••••";
/** Failures within this long of each other share one toast. */
const MERGE_MS = 2_000;
const TOAST_LINGER_MS = 10_000;
const OUTCOME_MS = 3_000;
const COPIED_MS = 2_000;
/** A person's action that the feed has not settled by now is settled from the POST's own answer. */
const SETTLE_MS = 5_000;
const RING_MS = 1_500;

export interface CredentialVM {
  readonly label: string;
  /** The mask, or the value while shown. Never put in an attribute. */
  readonly text: string;
  readonly shown: boolean;
  readonly copied: boolean;
}

export interface RowVM {
  readonly id: string;
  readonly name: string;
  readonly group: GroupKey;
  readonly time: string;
  readonly dot: Dot;
  readonly word: string;
  readonly what: string;
  readonly ports: readonly PortLink[];
  readonly error: string;
  readonly requestBadge: number;
  readonly credentialsCount: number;
  readonly credentialsOpen: boolean;
  readonly credentials: readonly CredentialVM[];
  readonly canStart: boolean;
  readonly canRestart: boolean;
  readonly canStop: boolean;
  /** A stop or restart waits for the person's confirmation. */
  readonly confirm: "stop" | "restart" | null;
  readonly confirmText: string;
  readonly busy: boolean;
  readonly refusal: { readonly code: string; readonly message: string } | null;
  /** A one-shot ring on the row (Show in panel). */
  readonly ringed: boolean;
}

export interface RequestVM {
  readonly id: string;
  readonly action: string;
  readonly serviceName: string;
  readonly agent: string;
  readonly summary: string;
  readonly reason: string;
  readonly busy: boolean;
  readonly refusal: { readonly code: string; readonly message: string } | null;
  /** The outcome line after an answer; "" while pending. */
  readonly outcome: string;
}

export interface ToastVM {
  readonly count: number;
  readonly text: string;
  readonly context: string;
  readonly serviceId: string;
}

export interface SeloVM {
  readonly value: number;
  readonly tone: "danger" | "attention" | "";
  readonly label: string;
}

export interface PanelViewModel {
  readonly loading: boolean;
  readonly error: string;
  readonly live: boolean;
  readonly mode: PanelMode;
  readonly open: boolean;
  readonly selo: SeloVM;
  readonly groups: readonly { readonly key: GroupKey; readonly label: string; readonly rows: readonly RowVM[] }[];
  /** One dot per service, for the collapsed handle. */
  readonly dots: readonly Dot[];
  readonly requests: readonly RequestVM[];
  readonly empty: boolean;
  readonly toast: ToastVM | null;
  readonly logOpen: boolean;
  /** The service the person is sent to by the toast, until the view has scrolled to it. */
  readonly focusId: string;
  /** The handle rings once when something new arrives. */
  readonly signal: number;
  readonly announcement: string;
}

export interface PanelDeps {
  readonly source: ServicesSource;
  readonly preferences: PanelPreferences;
  readonly clipboard: Clipboard;
  readonly scheduler: Scheduler;
  readonly projectId: string;
  readonly taskId: string;
  readonly taskTitle: string;
}

const signature = (s: Service): string => `${s.status}|${s.run?.startedAt ?? ""}|${s.run?.endedAt ?? ""}|${s.run?.exitCode ?? ""}`;

export function seloOf(falhas: number, requests: number): SeloVM {
  const value = falhas + requests;
  const parts: string[] = [];
  if (falhas > 0) parts.push(`${falhas} failed service${falhas === 1 ? "" : "s"}`);
  if (requests > 0) parts.push(`${requests} request${requests === 1 ? "" : "s"} waiting on you`);
  return { value, tone: falhas > 0 ? "danger" : requests > 0 ? "attention" : "", label: parts.join(", ") };
}

/** S toggles the panel from the whiteboard, unless the person is typing or holds a modifier. */
export function isToggleKey(event: { key: string; metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; target?: unknown }): boolean {
  if (event.key.toLowerCase() !== "s" || event.metaKey || event.ctrlKey || event.altKey) return false;
  const target = (event.target ?? {}) as { tagName?: string; isContentEditable?: boolean };
  const tag = target.tagName?.toLowerCase();
  return !(tag === "input" || tag === "textarea" || tag === "select" || target.isContentEditable);
}

export class PanelPresenter extends Presenter<PanelViewModel> {
  readonly #d: PanelDeps;
  readonly #services = new Map<string, Service>();
  #requests: ServiceRequest[] = [];
  #seq = 0;
  #loaded = false;
  #loading = false;
  #queued: ServicesEvent[] = [];
  #stopFollow: () => void = () => {};
  #mode: PanelMode = "docked";
  #sessionPane = false;
  /** The person's choice (stored); null until they make one. */
  #choice: boolean | null;
  #defaultOpen = false;
  #overlayOpen = false;
  #confirm = new Map<string, "stop" | "restart">();
  #busy = new Map<string, { verb: ServiceVerb; sig: string }>();
  #refusals = new Map<string, { code: string; message: string }>();
  #answering = new Set<string>();
  readonly #cards = new Map<string, ServiceRequest | undefined>();
  #outcomes = new Map<string, string>();
  #requestRefusals = new Map<string, { code: string; message: string }>();
  #credOpen = new Set<string>();
  #shown = new Set<string>();
  #copied = new Set<string>();
  #ring = "";
  #focusId = "";
  #signal = 0;
  #toast: { ids: string[]; count: number; first: Service; lastAt: number } | null = null;
  #cancelToast: (() => void) | null = null;
  #log: LogPresenter | null = null;
  #logSubscribers = new Set<(log: LogPresenter | null) => void>();
  #announcement = "";

  constructor(deps: PanelDeps) {
    super({
      loading: true, error: "", live: false, mode: "docked", open: false, selo: seloOf(0, 0), groups: [], dots: [], requests: [], empty: false,
      toast: null, logOpen: false, focusId: "", signal: 0, announcement: "",
    });
    this.#d = deps;
    this.#choice = deps.preferences.open(deps.taskId);
  }

  /** The log presenter while a log is open. */
  get log(): LogPresenter | null {
    return this.#log;
  }

  async start(): Promise<void> {
    this.#stopFollow = this.#d.source.follow(this.#d.projectId, {
      onEvent: (event) => this.#onEvent(event),
      onStatus: (status) => this.#onStatus(status),
    });
    await this.#load();
  }

  stop(): void {
    this.#stopFollow();
    this.#log?.stop();
    this.#cancelToast?.();
  }

  // ── layout ──────────────────────────────────────────────────────────────
  setMode(mode: PanelMode): void {
    if (mode === this.#mode) return;
    this.#mode = mode;
    this.#overlayOpen = false;
    this.#refresh();
  }

  /** Whether a session pane is open beside the board: the docked panel then starts collapsed. */
  setSessionPane(open: boolean): void {
    if (open === this.#sessionPane) return;
    this.#sessionPane = open;
    this.#defaultOpen = this.#computeDefault();
    this.#refresh();
  }

  toggle(): void {
    this.#setOpen(!this.#isOpen(), true);
  }

  /** Opens the panel without remembering it as the person's choice (the toast's actions). */
  open(): void {
    this.#setOpen(true, false);
  }

  collapse(): void {
    this.#setOpen(false, true);
  }

  #setOpen(open: boolean, remember: boolean): void {
    if (this.#mode === "docked") {
      this.#choice = open;
      if (remember) this.#d.preferences.saveOpen(this.#d.taskId, open);
    } else this.#overlayOpen = open;
    this.#refresh();
  }

  #isOpen(): boolean {
    if (!this.#loaded) return false;
    return this.#mode === "docked" ? (this.#choice ?? this.#defaultOpen) : this.#overlayOpen;
  }

  #computeDefault(): boolean {
    return !this.#sessionPane && (this.#services.size > 0 || this.#requests.length > 0);
  }

  /** Escape: cancels a confirmation, then closes the log, then collapses an overlay. Returns whether it was used. */
  key(key: string): boolean {
    if (key !== "Escape") return false;
    if (this.#confirm.size > 0) {
      this.#confirm.clear();
    } else if (this.#log) {
      this.closeLog();
      return true;
    } else if (this.#mode !== "docked" && this.#overlayOpen) {
      this.collapse();
      return true;
    } else return false;
    this.#refresh();
    return true;
  }

  tick(): void {
    this.#refresh();
  }

  // ── loading and the feed ────────────────────────────────────────────────
  async #load(): Promise<void> {
    if (this.#loading) return;
    this.#loading = true;
    try {
      const [list, requests] = await Promise.all([this.#d.source.list(this.#d.projectId, this.#d.taskId), this.#d.source.requests(this.#d.projectId, this.#d.taskId)]);
      this.#services.clear();
      for (const s of list.services) if (belongsToTask(s, this.#d.taskId)) this.#services.set(s.id, s);
      this.#requests = requests.filter((r) => r.taskId === this.#d.taskId && r.state === "pending");
      this.#seq = list.seq;
      const first = !this.#loaded;
      this.#loaded = true;
      if (first) this.#defaultOpen = this.#computeDefault();
      this.update((vm) => ({ ...vm, error: "" }));
    } catch (err) {
      this.#loading = false;
      this.update((vm) => ({ ...vm, loading: false, error: err instanceof Error ? err.message : "The services did not load." }));
      return;
    }
    this.#loading = false;
    const queued = this.#queued.splice(0).sort((a, b) => a.seq - b.seq);
    // A queued event past a gap now is not worth another list: the list is as new as the server could make it.
    for (const event of queued) if (event.seq <= this.#seq + 1) this.#onEvent(event);
    this.#refresh();
  }

  retry(): void {
    void this.#load();
  }

  #onStatus(status: LiveStatus): void {
    this.update((vm) => ({ ...vm, live: status === "live" }));
    if (status === "resynced") void this.#load();
  }

  #onEvent(event: ServicesEvent): void {
    if (!this.#loaded || this.#loading) {
      this.#queued.push(event);
      return;
    }
    if (event.seq <= this.#seq) return;
    if (event.seq > this.#seq + 1) {
      this.#queued.push(event);
      void this.#load();
      return;
    }
    this.#seq = event.seq;
    if (event.kind === "service_changed") this.#serviceChanged(event.service);
    else if (event.kind === "service_deleted") {
      this.#services.delete(event.serviceId);
      this.#busy.delete(event.serviceId);
      if (this.#log?.serviceId === event.serviceId) this.closeLog();
    } else if (event.kind === "service_request") this.#requestChanged(event.request);
    this.#refresh();
  }

  #serviceChanged(next: Service): void {
    const prev = this.#services.get(next.id);
    if (!belongsToTask(next, this.#d.taskId)) {
      if (prev) this.#services.delete(next.id);
      return;
    }
    this.#services.set(next.id, next);
    if (!prev) this.#signal++;
    const waiting = this.#busy.get(next.id);
    if (waiting && waiting.sig !== signature(next)) this.#busy.delete(next.id);
    if (isFalha(next) && !(prev && isFalha(prev))) this.#failed(next);
  }

  #requestChanged(request: ServiceRequest): void {
    if (request.taskId !== this.#d.taskId) return;
    const known = this.#requests.some((r) => r.id === request.id);
    if (request.state !== "pending") {
      this.#requests = this.#requests.filter((r) => r.id !== request.id);
      this.#answering.delete(request.id);
      return;
    }
    if (known) this.#requests = this.#requests.map((r) => (r.id === request.id ? request : r));
    else {
      this.#requests = [...this.#requests, request];
      this.#signal++;
    }
  }

  // ── the falha toast ─────────────────────────────────────────────────────
  #failed(service: Service): void {
    const now = this.#d.scheduler.now();
    const t = this.#toast;
    if (t && now - t.lastAt <= MERGE_MS) {
      if (!t.ids.includes(service.id)) t.ids.push(service.id);
      t.count = t.ids.length;
      t.lastAt = now;
    } else {
      this.#toast = { ids: [service.id], count: 1, first: service, lastAt: now };
    }
    this.#cancelToast?.();
    this.#cancelToast = null;
    this.#announcement = `${service.name} failed`;
  }

  dismissToast(): void {
    this.#cancelToast?.();
    this.#cancelToast = null;
    this.#toast = null;
    this.#refresh();
  }

  /** The pointer or focus is on the toast (or has left it): it goes 10s after the last of them leaves. */
  toastEngaged(engaged: boolean): void {
    this.#cancelToast?.();
    this.#cancelToast = engaged ? null : this.#d.scheduler.after(TOAST_LINGER_MS, () => this.dismissToast());
  }

  showInPanel(): void {
    const t = this.#toast;
    this.open();
    this.#focusId = t ? (t.count > 1 ? "" : t.ids[0]!) : "";
    this.#ring = this.#focusId;
    if (this.#ring !== "") {
      const ringed = this.#ring;
      this.#d.scheduler.after(RING_MS, () => {
        if (this.#ring === ringed) this.#ring = "";
        this.#refresh();
      });
    }
    this.dismissToast();
  }

  openLogFromToast(): void {
    const id = this.#toast?.ids[0];
    this.open();
    this.dismissToast();
    if (id) this.openLog(id);
  }

  focused(): void {
    this.#focusId = "";
    this.#refresh();
  }

  // ── logs ────────────────────────────────────────────────────────────────
  onLogChange(listener: (log: LogPresenter | null) => void): () => void {
    this.#logSubscribers.add(listener);
    return () => void this.#logSubscribers.delete(listener);
  }

  openLog(serviceId: string): void {
    if (!this.#services.has(serviceId)) return;
    this.#log?.stop();
    this.#log = new LogPresenter(this.#d.source, serviceId);
    void this.#log.start();
    this.#notifyLog();
    this.#refresh();
  }

  closeLog(): void {
    this.#log?.stop();
    this.#log = null;
    this.#notifyLog();
    this.#refresh();
  }

  #notifyLog(): void {
    for (const l of [...this.#logSubscribers]) l(this.#log);
  }

  // ── a person's actions ──────────────────────────────────────────────────
  /** Start goes straight through; stop and restart ask first, inside the row. */
  request(serviceId: string, verb: ServiceVerb): void {
    if (this.#busy.has(serviceId)) return;
    if (verb === "start") {
      void this.#run(serviceId, verb);
      return;
    }
    this.#confirm.clear();
    this.#confirm.set(serviceId, verb);
    this.#refresh();
  }

  cancel(serviceId: string): void {
    this.#confirm.delete(serviceId);
    this.#refresh();
  }

  confirm(serviceId: string): void {
    const verb = this.#confirm.get(serviceId);
    this.#confirm.delete(serviceId);
    if (verb) void this.#run(serviceId, verb);
    else this.#refresh();
  }

  async #run(serviceId: string, verb: ServiceVerb): Promise<void> {
    const service = this.#services.get(serviceId);
    if (!service || this.#busy.has(serviceId)) return;
    this.#refusals.delete(serviceId);
    this.#busy.set(serviceId, { verb, sig: signature(service) });
    this.#refresh();
    try {
      const answer = await this.#d.source.act(verb, serviceId, this.#d.taskId);
      // The feed settles the row. If it already has, the busy mark is gone; if it never does, the answer settles it.
      if (this.#busy.has(serviceId)) {
        this.#d.scheduler.after(SETTLE_MS, () => {
          if (!this.#busy.has(serviceId)) return;
          this.#busy.delete(serviceId);
          if (belongsToTask(answer, this.#d.taskId)) this.#services.set(serviceId, answer);
          this.#refresh();
        });
      }
    } catch (err) {
      this.#busy.delete(serviceId);
      this.#refusals.set(serviceId, { code: errorCode(err), message: err instanceof Error ? err.message : "The action was refused." });
    }
    this.#refresh();
  }

  dismissRefusal(serviceId: string): void {
    this.#refusals.delete(serviceId);
    this.#refresh();
  }

  // ── pedidos de serviço ──────────────────────────────────────────────────
  async answer(requestId: string, decision: Decision): Promise<void> {
    if (this.#answering.has(requestId) || !this.#requests.some((r) => r.id === requestId)) return;
    this.#answering.add(requestId);
    this.#requestRefusals.delete(requestId);
    this.#refresh();
    try {
      await this.#d.source.answer(requestId, decision);
      this.#answering.delete(requestId);
      const card = this.#requests.find((r) => r.id === requestId);
      this.#requests = this.#requests.filter((r) => r.id !== requestId);
      this.#outcomes.set(requestId, `${decision === "approve" ? "Approved" : "Denied"}${card ? ` · ${card.serviceName}` : ""}`);
      this.#cards.set(requestId, card);
      this.#d.scheduler.after(OUTCOME_MS, () => {
        this.#outcomes.delete(requestId);
        this.#cards.delete(requestId);
        this.#refresh();
      });
    } catch (err) {
      this.#answering.delete(requestId);
      this.#requestRefusals.set(requestId, { code: errorCode(err), message: err instanceof Error ? err.message : "The answer was refused." });
    }
    this.#refresh();
  }

  // ── credentials ─────────────────────────────────────────────────────────
  toggleCredentials(serviceId: string): void {
    if (!this.#credOpen.delete(serviceId)) this.#credOpen.add(serviceId);
    this.#refresh();
  }

  toggleShown(serviceId: string, index: number): void {
    const key = `${serviceId}:${index}`;
    if (!this.#shown.delete(key)) this.#shown.add(key);
    this.#refresh();
  }

  async copy(serviceId: string, index: number): Promise<void> {
    const credential = this.#services.get(serviceId)?.credentials[index];
    if (!credential) return;
    try {
      await this.#d.clipboard.copy(credential.value);
    } catch {
      return;
    }
    const key = `${serviceId}:${index}`;
    this.#copied.add(key);
    this.#refresh();
    this.#d.scheduler.after(COPIED_MS, () => {
      this.#copied.delete(key);
      this.#refresh();
    });
  }

  // ── the view model ──────────────────────────────────────────────────────
  #refresh(): void {
    const services = [...this.#services.values()];
    const falhas = services.filter(isFalha).length;
    const now = this.#d.scheduler.now();
    const row = (s: Service, group: GroupKey): RowVM => {
      const status = statusView(s);
      const busy = this.#busy.get(s.id);
      const confirm = this.#confirm.get(s.id) ?? null;
      const busyWord = busy ? { stop: "stopping…", restart: "restarting…", start: "starting…" }[busy.verb] : "";
      return {
        id: s.id,
        name: s.name,
        group,
        time: timeFact(s, now),
        dot: busy ? "pending" : status.dot,
        word: busy ? busyWord : status.word,
        what: whatItRuns(s),
        ports: portLinks(s),
        error: group === "attention" ? firstErrorLine(s) : "",
        requestBadge: this.#requests.filter((r) => r.serviceId === s.id).length,
        credentialsCount: s.credentials.length,
        credentialsOpen: this.#credOpen.has(s.id),
        credentials: s.credentials.map((c, i) => {
          const shown = this.#shown.has(`${s.id}:${i}`);
          return { label: c.label, text: shown ? c.value : MASK, shown, copied: this.#copied.has(`${s.id}:${i}`) };
        }),
        canStart: neverRan(s),
        canRestart: !neverRan(s),
        canStop: s.status === "running" || s.status === "starting",
        confirm,
        confirmText:
          confirm === "stop"
            ? `Stop ${s.name}? The run ends now${s.run?.startedBy.agent ? `; ${s.run.startedBy.agent} may be using it` : ""}. Logs stay.`
            : confirm === "restart"
              ? `Restart ${s.name}? It starts again on the same worktree.`
              : "",
        busy: busy !== undefined,
        refusal: this.#refusals.get(s.id) ?? null,
        ringed: this.#ring === s.id,
      };
    };
    const groups = groupServices(services).map((g) => ({ key: g.key, label: g.label, rows: g.services.map((s) => row(s, g.key)) }));
    const pending: RequestVM[] = this.#requests.map((r) => ({
      id: r.id,
      action: r.action,
      serviceName: r.serviceName,
      agent: r.agent,
      summary: r.summary,
      reason: r.reason,
      busy: this.#answering.has(r.id),
      refusal: this.#requestRefusals.get(r.id) ?? null,
      outcome: "",
    }));
    const answered: RequestVM[] = [...this.#outcomes].map(([id, outcome]) => {
      const card = this.#cards.get(id);
      return { id, action: card?.action ?? "", serviceName: card?.serviceName ?? "", agent: card?.agent ?? "", summary: "", reason: "", busy: false, refusal: null, outcome };
    });
    const t = this.#toast;
    const selo = seloOf(falhas, this.#requests.length);
    const previous = this.viewModel;
    this.set({
      loading: !this.#loaded && previous.error === "",
      error: previous.error,
      live: previous.live,
      mode: this.#mode,
      open: this.#isOpen(),
      selo,
      groups,
      dots: services.sort((a, b) => (a.name < b.name ? -1 : 1)).map((s) => statusView(s).dot),
      requests: [...answered, ...pending],
      empty: this.#loaded && services.length === 0 && this.#requests.length === 0,
      toast: t
        ? {
            count: t.count,
            serviceId: t.ids[0]!,
            text: t.count > 1 ? `${t.count} services failed` : `${t.first.name} failed${t.first.run?.exitCode ? ` · exit code ${t.first.run.exitCode}` : ""}`,
            context: `${this.#d.taskTitle}${t.first.run?.startedBy.agent ? ` · started by ${t.first.run.startedBy.agent}` : ""}`,
          }
        : null,
      logOpen: this.#log !== null,
      focusId: this.#focusId,
      signal: this.#signal,
      announcement: this.#announcement === "" ? (selo.label === "" ? "" : selo.label) : this.#announcement,
    });
  }
}

function errorCode(err: unknown): string {
  return err && typeof err === "object" && "code" in err && typeof err.code === "string" ? err.code : "ERROR";
}
