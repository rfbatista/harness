// The person's terminals of a session, as a dock under the Files viewer: one
// closable tab per terminal, labelled with its folder, each a terminal on its
// own socket (sessionsPersonTerminal, the session terminal's component). The
// dock lists the session's terminals on load, so a reload reattaches them; a
// terminal the server ended keeps its tab and says why; one the page holds
// that the server no longer lists says the harness restarted. The person sets
// its height by the handle on its top edge, or expands it over the Files tab;
// both are kept in this browser.
//
//   <section class="[ person-terms ]" x-data="sessionsPersonTerms(sid)" x-show="visible" x-bind:style="dockStyle"
//            x-on:person-terminal-open-requested.window="openRequested" x-on:terminal-status.stop>…</section>
//
// The Files tab's tree asks for a terminal with the window event
// person-terminal-open-requested ({ sessionId?, root, dir }), and hears the
// dock's person-terminals-count and person-terminals-expanded.

import { StructuredError } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { clampDock, DOCK_MIN_PX, endedNote, nextDockHeight, refusalCopy, RESTART_NOTE, SESSION_ENDED_NOTE } from "../../domain/personTerms.js";

const HEIGHT_KEY = "person-terms.height";
const EXPANDED_KEY = "person-terms.expanded";

const WHERE = { worktree: "Worktree", resources: "Resources" };

/**
 * @param {{
 *   persons: import("../../domain/ports.js").PersonTerminalGateway,
 *   prefs: import("../../../../shared/infrastructure/storage.js").Preferences,
 *   measure?: (el: Element | undefined) => { height: number },  the tab the dock sits in
 *   observeResize?: (el: Element, onResize: () => void) => () => void,
 * }} deps
 */
export const personTerms = ({ persons, prefs, measure = defaultMeasure, observeResize = resizeObserver }) => (sessionId = "") => {
  let stopFollowing = () => {};
  let stopObserving = () => {};
  let dragPointer = null;

  const toTab = (t) => ({
    id: t.id,
    root: t.root,
    dir: t.dir,
    label: t.label,
    where: WHERE[t.root] ?? t.root,
    readonly: t.root === "resources",
    ended: null,
  });

  return {
    sessionId,
    terms: [],
    currentId: "",
    opening: false,
    refusal: null,
    sessionEnded: false,
    /** What each terminal last said about its connection (terminal-status), by id. */
    states: {},
    dragging: false,
    height: readHeight(prefs),
    expanded: prefs.get(EXPANDED_KEY) === "1",

    init() {
      this.refreshRoom();
      if (this.$root?.parentElement) stopObserving = observeResize(this.$root.parentElement, () => this.refreshRoom());
      if (this.expanded) this.$dispatch("person-terminals-expanded", { sessionId, expanded: true });
      stopFollowing = persons.followEnded(sessionId, (e) => this.ended(e), (s) => this.feedStatus(s));
      return this.load();
    },

    destroy() {
      stopObserving();
      stopFollowing();
    },

    get current() {
      return this.terms.find((t) => t.id === this.currentId) ?? this.terms[0] ?? null;
    },
    get count() {
      return this.terms.filter((t) => !t.ended).length;
    },
    get visible() {
      return this.terms.length > 0 || this.refusal !== null;
    },
    /** What the tab row draws, one entry per terminal. */
    get tabs() {
      const current = this.current?.id;
      return this.terms.map((t) => ({
        id: t.id,
        label: t.label,
        where: t.where,
        locked: t.readonly,
        title: `${t.where} · ${t.dir || t.where}`,
        closeLabel: `Close terminal: ${t.label}`,
        isCurrent: t.id === current,
        currentAttr: t.id === current ? "" : null,
        endedAttr: t.ended ? "" : null,
        selected: t.id === current ? "true" : "false",
        tabindex: t.id === current ? "0" : "-1",
        note: t.ended,
        readonlyNote: t.readonly && !t.ended,
        reopen: Boolean(t.ended?.reopen),
        toneAttr: t.ended?.tone ?? null,
      }));
    },
    /** The connection of the terminal on show, when it is down: Reconnect is offered. */
    get down() {
      const t = this.current;
      return t && !t.ended && this.states[t.id] === "offline" ? t.id : "";
    },
    get expandLabel() {
      return this.expanded ? "Restore terminals" : "Expand terminals";
    },

    /** Lists what the server runs and reconciles the tabs with it. */
    async load() {
      let listed;
      try {
        listed = await persons.list(sessionId);
      } catch {
        return; // the list is shown as it was; the next resync corrects it
      }
      const ids = new Set(listed.map((t) => t.id));
      const held = this.terms.map((t) => (!t.ended && !ids.has(t.id) ? this.lost(t) : t));
      const known = new Set(held.map((t) => t.id));
      this.terms = [...held, ...listed.filter((t) => !known.has(t.id)).map(toTab)];
      if (!this.terms.some((t) => t.id === this.currentId)) this.currentId = this.terms[0]?.id ?? "";
      this.report();
    },

    /** A tab the server does not list: the session's end took it, or the harness restarted. */
    lost(t) {
      const note = this.sessionEnded && t.readonly
        ? endedNote({ reason: "root_gone", message: SESSION_ENDED_NOTE })
        : endedNote({ reason: "restarted", message: RESTART_NOTE });
      return { ...t, ended: note };
    },

    /** @param {import("../../domain/ports.js").PersonTerminalEnded} e */
    ended(e) {
      this.terms = this.terms.map((t) => (t.id === e.id ? { ...t, ended: endedNote(e) } : t));
      this.report();
    },

    /**
     * @param {CustomEvent<{ id: string, code: number }>} event  a shell's socket said exit. The server's event carries the
     * sentence to show and overrides this one; until it comes (or if the page missed it) the pane says what is known.
     */
    terminalExited(event) {
      const { id, code } = event.detail ?? {};
      this.terms = this.terms.map((t) => {
        if (t.id !== id || t.ended) return t;
        if (this.sessionEnded && t.readonly) return { ...t, ended: endedNote({ reason: "root_gone", message: SESSION_ENDED_NOTE }) };
        return { ...t, ended: endedNote({ reason: "exited", message: code ? `The shell exited with code ${code}.` : "The shell exited." }) };
      });
      this.report();
    },

    /** The session's end closes its resources terminals: say so at once, whether or not the event came. */
    setSessionEnded(ended) {
      this.sessionEnded = ended;
      if (!ended) return;
      this.terms = this.terms.map((t) => (!t.ended && t.readonly ? { ...t, ended: endedNote({ reason: "root_gone", message: SESSION_ENDED_NOTE }) } : t));
      this.report();
    },

    /** @param {import("../../../../shared/domain/feed.js").FeedStatus} status */
    feedStatus(status) {
      if (status === FeedStatus.RESYNCED) this.load();
    },

    /** @param {CustomEvent<{ sessionId?: string, root: string, dir: string }>} event  Open terminal here, from the tree */
    openRequested(event) {
      const d = event.detail ?? {};
      if (d.sessionId && d.sessionId !== sessionId) return;
      return this.open(d.root, d.dir ?? "");
    },

    /** Opens a terminal on a folder, in a slot of its own or the one `replacing` held. */
    async open(root, dir, replacing = "") {
      if (this.opening) return;
      this.opening = true;
      try {
        const t = toTab(await persons.open(sessionId, { root, dir, cols: 80, rows: 24 }));
        this.refusal = null;
        const at = this.terms.findIndex((x) => x.id === replacing);
        this.terms = at < 0 ? [...this.terms, t] : this.terms.map((x, i) => (i === at ? t : x));
        this.currentId = t.id;
      } catch (err) {
        const code = err instanceof StructuredError ? err.code : "";
        const copy = refusalCopy(code, err?.message ?? "");
        this.refusal = { code, ...copy };
      } finally {
        this.opening = false;
        this.report();
      }
    },

    /** Opens a new terminal where an ended one was, in its slot. */
    reopen(id) {
      const t = this.terms.find((x) => x.id === id);
      return t ? this.open(t.root, t.dir, id) : undefined;
    },

    select(id) {
      this.currentId = id;
    },

    /** Left and Right walk the tabs, Home and End go to the ends; the tab in focus is the one on show. */
    tabKey(event) {
      const at = this.terms.findIndex((t) => t.id === this.current?.id);
      const to = { ArrowRight: at + 1, ArrowLeft: at - 1, Home: 0, End: this.terms.length - 1 }[event.key];
      if (to === undefined || this.terms.length === 0) return;
      event.preventDefault();
      this.select(this.terms[(to + this.terms.length) % this.terms.length].id);
      this.$nextTick(() => this.$el?.querySelector?.('[role="tab"][tabindex="0"]')?.focus());
    },

    /** @param {CustomEvent<import("./terminal.js").TerminalStatus>} event  a terminal in the dock: its dot and Reconnect are the dock's, not the page's */
    terminalStatus(event) {
      const { id, state } = event.detail;
      if (this.states[id] !== state) this.states = { ...this.states, [id]: state };
    },

    reconnect() {
      if (this.down) this.$dispatch("terminal-reconnect-requested", { id: this.down });
    },

    /** Hangs up the shell and takes the tab away. */
    async close(id) {
      const at = this.terms.findIndex((t) => t.id === id);
      if (at < 0) return;
      const wasEnded = this.terms[at].ended !== null;
      this.terms = this.terms.filter((t) => t.id !== id);
      if (this.currentId === id) this.currentId = (this.terms[at] ?? this.terms[at - 1])?.id ?? "";
      this.report();
      if (!wasEnded) {
        try {
          await persons.close(id);
        } catch {
          // the tab is gone either way; the next list tells what the server runs
        }
      }
    },

    dismissRefusal() {
      this.refusal = null;
    },

    /** Tells the Files tab how many terminals run, for its badge. */
    report() {
      this.$dispatch("person-terminals-count", { sessionId, count: this.count });
    },

    // ── the dock's size ───────────────────────────────────────────────────────

    /** How tall the tab the dock sits in is, as last measured; 0 while it is hidden. */
    tabHeight: 0,
    /** The tab the dock sits in, as far as it reaches; unbounded while the tab is hidden and has no height to measure. */
    get room() {
      return this.tabHeight > 0 ? this.tabHeight : Number.POSITIVE_INFINITY;
    },
    refreshRoom() {
      this.tabHeight = measure(this.$root?.parentElement).height;
    },
    /** The dock's height in px, within its tab; the CSS default (a share of the tab) until the person sets one. */
    get dockStyle() {
      return this.height === null ? "" : `--dock: ${clampDock(this.height, this.room)}px`;
    },
    get handleValue() {
      const room = this.room;
      return Number.isFinite(room) ? Math.round((clampDock(this.height ?? 0, room) / room) * 100) : 0;
    },

    setHeight(px) {
      this.refreshRoom();
      this.height = clampDock(px, this.room);
      prefs.set(HEIGHT_KEY, String(Math.round(this.height)));
    },

    handleKey(event) {
      const next = nextDockHeight(event.key, this.height ?? (Number.isFinite(this.room) ? this.room * 0.48 : DOCK_MIN_PX), this.room);
      if (next === null) return;
      event.preventDefault();
      this.setHeight(next);
    },

    dragStart(event) {
      dragPointer = event.pointerId;
      event.currentTarget.setPointerCapture?.(event.pointerId);
      this.dragging = true;
    },

    /** @param {{ clientY: number }} event  @param {{ top: number, bottom: number }} [tab]  the tab's box; measured when not given */
    dragMove(event, tab = this.$root?.parentElement?.getBoundingClientRect()) {
      if (!this.dragging || !tab) return;
      this.setHeight(tab.bottom - event.clientY);
    },

    dragEnd(event) {
      if (!this.dragging) return;
      event.currentTarget?.releasePointerCapture?.(dragPointer);
      dragPointer = null;
      this.dragging = false;
    },

    toggleExpanded() {
      this.expanded = !this.expanded;
      prefs.set(EXPANDED_KEY, this.expanded ? "1" : null);
      this.$dispatch("person-terminals-expanded", { sessionId, expanded: this.expanded });
    },

    /** Escape restores an expanded dock. */
    escape() {
      if (this.expanded) this.toggleExpanded();
    },
  };
};

function readHeight(prefs) {
  const n = Number(prefs.get(HEIGHT_KEY));
  return Number.isFinite(n) && n > 0 ? n : null;
}

function resizeObserver(el, onResize) {
  if (typeof ResizeObserver === "undefined") return () => {};
  const observer = new ResizeObserver(() => onResize());
  observer.observe(el);
  return () => observer.disconnect();
}

function defaultMeasure(el) {
  return { height: el?.clientHeight ?? 0 };
}
