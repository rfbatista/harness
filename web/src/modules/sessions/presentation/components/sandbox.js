// A session's Sandbox tab (UX 7): the session's sandbox facts, then the
// denied access the sandbox recorded for it, filtered, grouped and one row at
// a time opened. Nothing here changes the allowlist: "Allow path" is a link
// into the Agent isolation settings.
//
//   <template x-for="panel in sandboxPanels" x-bind:key="panel.key">
//     <section x-data="sessionsSandbox(panel)"> …
//
// Mounted only while the tab is in front. The facts show at once; the log
// loads beside them. While the session runs it is read again every few
// seconds, and the rows the reader has open stay open.

import { describeError } from "../../../../shared/presentation/errors.js";
import { filterViolations, summarize, violationRows } from "../../domain/sandbox.js";
import { rowView } from "../sandboxView.js";

/** How often a running session's log is read again. */
export const POLL_MS = 4_000;

/** @param {{ sandbox: import("../../domain/ports.js").SandboxGateway, timers?: typeof globalThis, clipboard?: { writeText(text: string): Promise<void> } | null }} deps */
export const sandbox = ({ sandbox: gateway, timers = globalThis, clipboard = globalThis.navigator?.clipboard ?? null }) => (panel = {}) => {
  let pollTimer = 0;
  let gone = false;

  return {
    sessionId: panel.sessionId ?? "",
    live: panel.live === true,
    facts: panel.facts ?? { kind: "legacy" },
    /** loading → ready, or failed; unavailable where the platform collects none. */
    state: "loading",
    error: "",
    /** @type {readonly import("../../domain/sandbox.js").Violation[]} */
    violations: [],
    access: "all",
    query: "",
    grouped: true,
    openKey: "",

    // ── what the markup binds ────────────────────────────────────────────
    get isLoading() {
      return this.state === "loading";
    },
    get isFailed() {
      return this.state === "failed";
    },
    get isUnavailable() {
      return this.state === "unavailable";
    },
    /** A session that ran without the sandbox, or before it, has no log to read. */
    get hasLog() {
      return this.facts.kind === "on";
    },
    get isEmpty() {
      return this.hasLog && this.state === "ready" && this.violations.length === 0;
    },
    get hasList() {
      return this.hasLog && this.state === "ready" && this.violations.length > 0;
    },
    get filtered() {
      return filterViolations(this.violations, { access: this.access, query: this.query });
    },
    get rows() {
      return violationRows(this.filtered, this.grouped).map(rowView);
    },
    get noMatch() {
      return this.hasList && this.rows.length === 0;
    },
    get summary() {
      const { denied, paths } = summarize(this.violations);
      return `· ${denied} denied, ${paths} different ${paths === 1 ? "path" : "paths"}`;
    },
    get open() {
      return this.rows.find((r) => r.key === this.openKey) ?? null;
    },
    get hasOpen() {
      return this.open !== null;
    },
    get hasHint() {
      return (this.open?.hint ?? "") !== "";
    },
    get canAllow() {
      return (this.open?.allowHref ?? "") !== "";
    },
    isOpen(row) {
      return row.key === this.openKey;
    },
    isAccess(value) {
      return this.access === value;
    },
    get footer() {
      return `Showing the latest ${this.violations.length}. Older ones are dropped.${this.live ? " Updates live while the session runs." : ""}`;
    },

    // ── actions ──────────────────────────────────────────────────────────
    setAccess(value) {
      this.access = value;
    },
    toggleGrouped() {
      this.grouped = !this.grouped;
    },
    toggle(row) {
      this.openKey = this.openKey === row.key ? "" : row.key;
    },
    clearFilters() {
      this.access = "all";
      this.query = "";
    },
    retry() {
      this.state = "loading";
      this.error = "";
      return this.load();
    },
    async copyPath(row, event) {
      return this.copy(row.path, event?.currentTarget);
    },
    async copyText(event) {
      return this.copy(this.rows.map((r) => r.plain).join("\n"), event?.currentTarget);
    },
    async copy(text, button) {
      if (!clipboard) return;
      try {
        await clipboard.writeText(text);
      } catch {
        return;
      }
      if (!button) return;
      const label = button.textContent;
      button.textContent = "Copied";
      timers.setTimeout(() => {
        button.textContent = label;
      }, 1_200);
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      if (!this.hasLog) return;
      this.load();
    },
    destroy() {
      gone = true;
      timers.clearTimeout(pollTimer);
    },
    async load() {
      try {
        const log = await gateway.violations(this.sessionId);
        if (gone) return;
        this.violations = log.violations;
        this.state = log.available ? "ready" : "unavailable";
        this.error = "";
      } catch (err) {
        if (gone) return;
        // A refresh that fails leaves what is shown; only a first load fails the panel.
        if (this.state === "loading" || this.state === "failed") {
          this.state = "failed";
          this.error = describeError(err).code;
        }
      }
      this.schedule();
    },
    schedule() {
      timers.clearTimeout(pollTimer);
      if (gone || !this.live || this.state === "unavailable" || this.state === "failed") return;
      pollTimer = timers.setTimeout(() => this.load(), POLL_MS);
    },
  };
};
