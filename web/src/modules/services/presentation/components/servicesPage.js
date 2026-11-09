// The Services page: every service of the project with its live status. The
// server seeds the list (the answer of list_services); the page then follows
// the project's services feed. An event at or below the list's seq is already
// in it; a gap, or a resynced feed, reads the list again (contract: Harness
// server ↔ Web UI — services, section 6).
//
//   <main x-data="servicesPage" data-seed="services-seed"> …

import { FeedStatus } from "../../../../shared/domain/feed.js";
import { branchLabel, worktreeKindWord } from "../../../../shared/domain/runWorktree.js";
import { systemClock } from "../../../../shared/infrastructure/clock.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { runLine } from "../../domain/log.js";
import { afterFailedRead, afterPick, afterRead, freshRunIn, hasRunIn, pickKeyOf, pickedWorktree, startKey } from "../../domain/runIn.js";
import { applyEvent, groupServices, isActive, runningCount, savedNotice, statusView, upsertService } from "../../domain/service.js";
import { trapTab } from "../focusTrap.js";
import { runInView, serviceRow } from "../view.js";

/** How often the times on the rows move on. */
const TICK_MS = 15_000;

const noPrefs = { get: () => null, set() {} };

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").ServicesGateway,
 *   clock?: import("../../../../shared/infrastructure/clock.js").Clock,
 *   timers?: { setInterval: typeof setInterval, clearInterval: typeof clearInterval },
 *   prefs?: import("../../../../shared/infrastructure/storage.js").Preferences,  the browser's own choices: each service's "Run in" pick
 * }} deps
 */
export const servicesPage = ({ gateway, clock = systemClock, timers = globalThis, prefs = noPrefs }) => () => {
  let unfollow = () => {};
  let ticker = null;

  return {
    projectId: "",
    /** @type {import("../../domain/ports.js").Service[]} */
    services: [],
    /** The services feed's sequence number the list is current to. */
    seq: 0,
    /** @type {{ name: "docker" | "podman", available: boolean }} */
    runtime: { name: "docker", available: true },
    /** @type {"on" | "off" | "unavailable"} */
    sandbox: "on",
    /** The time the rows are drawn at. */
    now: clock.now(),
    loading: false,
    live: false,
    error: null,
    /** The action in flight per service id: "start", "stop" or "restart". */
    flights: {},
    /** The "Run in" pick per service id that offers one (domain/runIn.js). */
    runIns: {},
    /** The refusal of the last action, {title, message, code, next, links}; null for none. */
    refusal: null,
    /** The service whose log is open (the log viewer follows it). */
    logId: "",
    /** The form drawer: "" closed, "new", or "edit" of formId. */
    formMode: "",
    formId: "",
    /** The delete asked about: {id, name, error}; null for none. */
    del: null,
    deleting: false,
    /** The control that opened a dialog, to give the focus back. */
    opener: null,
    /** Said politely to screen readers after a save. */
    notice: "",

    // ── what the markup binds ────────────────────────────────────────────
    get groups() {
      return groupServices(this.services).map((g) => ({
        key: g.key,
        label: g.label,
        tone: g.key === "running" ? "signal" : "",
        count: g.services.length,
        rows: g.services.map((s) => ({
          ...serviceRow(s, this.now, { flight: this.flights[s.id] ?? "", runtime: this.runtime }),
          runIn: runInView(s, this.runIns[s.id]),
          current: s.id === this.logId,
          canDelete: !isActive(s),
        })),
      }));
    },
    get empty() {
      return !this.loading && this.error === null && this.services.length === 0;
    },
    get skeleton() {
      return this.loading && this.services.length === 0;
    },
    get runningCount() {
      return runningCount(this.services);
    },
    get hasRunning() {
      return this.runningCount > 0;
    },
    // ── the form and the delete dialog ───────────────────────────────────
    get formOpen() {
      return this.formMode !== "";
    },
    /** The service the form edits, for the form's own setup; null for a new one. */
    get formService() {
      return this.formMode === "edit" ? (this.services.find((s) => s.id === this.formId) ?? null) : null;
    },
    get deleteOpen() {
      return this.del !== null;
    },
    get deleteTitle() {
      return `Delete ${this.del?.name ?? ""}?`;
    },
    get deleteHasError() {
      return (this.del?.error ?? null) !== null;
    },
    get deleteBusyLabel() {
      return this.deleting ? "Deleting…" : "Delete service";
    },

    // ── the log beside the list ──────────────────────────────────────────
    get detailOpen() {
      return this.logView !== null;
    },
    get noDetail() {
      return !this.detailOpen;
    },
    /** The open log's service, for its header. */
    get logView() {
      const s = this.services.find((x) => x.id === this.logId);
      return s ? { id: s.id, name: s.name, status: statusView(s), runLine: runLine(s.run) } : null;
    },
    /** The ids to draw a viewer for: one, so a new id makes a new viewer. */
    get logIds() {
      return this.logView ? [this.logView.id] : [];
    },

    get notices() {
      const notices = [];
      if (this.sandbox === "unavailable") {
        notices.push({
          key: "sandbox-unavailable",
          title: "The sandbox is unavailable on this machine.",
          message: "Command services cannot start, and nothing runs unconfined. Image services are not affected.",
          code: "SANDBOX_UNAVAILABLE",
          next: "turn the sandbox off in Settings → Agent isolation to run command services unconfined",
          href: "/agent-isolation#sandbox",
          action: "Agent isolation",
        });
      } else if (this.sandbox === "off") {
        notices.push({
          key: "sandbox-off",
          title: "The sandbox is off.",
          message: "Command services run unconfined and can read and write anything you can. Image services stay inside their container.",
          code: "",
          next: "",
          href: "/agent-isolation#sandbox",
          action: "Agent isolation",
        });
      }
      if (!this.runtime.available && this.services.some((s) => s.kind === "image")) {
        notices.push({
          key: "runtime-missing",
          title: `${this.runtime.name} was not found on this machine.`,
          message: `Image services cannot start. Install ${this.runtime.name}, or choose ${this.runtime.name === "docker" ? "podman" : "docker"} in the server settings.`,
          code: "RUNTIME_UNAVAILABLE",
          next: "Server settings → Services → Container runtime",
          href: "/server",
          action: "Server settings",
        });
      }
      return notices.map((n) => ({ ...n, hasCode: n.code !== "" }));
    },
    get hasNotices() {
      return this.notices.length > 0;
    },
    get runningLabel() {
      return `${this.runningCount} running`;
    },

    // ── lifecycle ────────────────────────────────────────────────────────
    init() {
      try {
        const seed = gateway.decodeSeed(readSeed(this.$el));
        this.projectId = seed.projectId;
        if (seed.list) this.show(seed.list);
      } catch (err) {
        this.error = describeError(err);
        return;
      }
      if (this.seq === 0 && this.services.length === 0) this.load();
      unfollow = gateway.follow(
        this.projectId,
        (event) => this.arrived(event),
        (status) => this.feedStatus(status),
      );
      ticker = timers.setInterval(() => { this.now = clock.now(); }, TICK_MS);
    },

    destroy() {
      unfollow();
      if (ticker !== null) timers.clearInterval(ticker);
    },

    feedStatus(status) {
      this.live = status === FeedStatus.LIVE;
      if (status === FeedStatus.RESYNCED) this.load();
    },

    /** One event of the feed. */
    arrived(event) {
      const next = applyEvent({ seq: this.seq, services: this.services }, event);
      if (next.reload) {
        this.load();
        return;
      }
      if (!next.applied) return;
      this.services = next.state.services;
      this.seq = next.state.seq;
      this.readRunIns();
      if (this.logId !== "" && !this.services.some((x) => x.id === this.logId)) this.closeLog();
    },

    show(list) {
      this.services = list.services;
      this.seq = list.seq;
      this.runtime = list.runtime;
      this.sandbox = list.sandbox;
      this.readRunIns();
    },

    /** Reads the list. Events the feed delivers meanwhile are at or below its seq, or a gap again. */
    async load() {
      this.loading = true;
      this.error = null;
      try {
        this.show(await gateway.listServices(this.projectId));
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.loading = false;
      }
    },

    // ── start, stop, restart ─────────────────────────────────────────────

    start(id) {
      return this.act("start", id);
    },
    stop(id) {
      return this.act("stop", id);
    },
    restart(id) {
      return this.act("restart", id);
    },

    /**
     * One action on one service. A service has at most one in flight, and the feed
     * may deliver its echo before the answer: an answer that comes after a newer event
     * for the same service is not applied over it.
     */
    async act(verb, id) {
      const service = this.services.find((s) => s.id === id);
      if (!service || this.flights[id]) return;
      if (verb === "start" && isActive(service)) {
        this.openLog(id);
        return;
      }
      if (verb === "start") this.refusal = null;
      this.flights = { ...this.flights, [id]: verb };
      const seqBefore = this.seq;
      try {
        if (verb === "start") {
          const answer = await gateway.startService(id, this.runIns[id] ? startKey(this.runIns[id]) : "");
          if (answer.alreadyRunning) this.openLog(id);
          this.settle(answer.service, seqBefore);
        } else {
          this.settle(await (verb === "stop" ? gateway.stopService(id) : gateway.restartService(id)), seqBefore);
        }
      } catch (err) {
        const shown = describeError(err);
        this.refusal = { ...shown, title: `${service.name} did not ${verb === "restart" ? "restart" : verb}.` };
        if (shown.code === "SERVICE_NOT_FOUND") this.load();
        // The worktree is gone: the refusal says so already, so the list is read again without a second notice.
        if (shown.code === "WORKTREE_NOT_AVAILABLE") await this.loadRunIn(id, { silent: true });
      } finally {
        const { [id]: _done, ...rest } = this.flights;
        this.flights = rest;
      }
    },

    /** The service an action answered with joins the list, unless the feed has moved on since. */
    settle(service, seqBefore) {
      if (this.seq !== seqBefore) return;
      this.services = upsertService(this.services, service);
    },

    // ── "Run in" ─────────────────────────────────────────────────────────

    /** Reads the run worktrees of each service that offers a pick and has none yet. */
    readRunIns() {
      for (const s of this.services) {
        if (!hasRunIn(s) || this.runIns[s.id]) continue;
        this.runIns = { ...this.runIns, [s.id]: freshRunIn() };
        this.loadRunIn(s.id);
      }
    },

    /** Reads one service's list: on load (the first list restores the remembered pick), when the picker opens, and after a refusal. */
    async loadRunIn(id, { silent = false } = {}) {
      let worktrees;
      try {
        worktrees = await gateway.listRunWorktrees(id);
      } catch {
        if (this.runIns[id]) this.runIns = { ...this.runIns, [id]: afterFailedRead(this.runIns[id]) };
        return;
      }
      const prev = this.runIns[id];
      if (!prev) return;
      const { next, clearStored } = afterRead(prev, worktrees, { stored: prefs.get(pickKeyOf(id)), silent });
      if (clearStored) prefs.set(pickKeyOf(id), null);
      this.runIns = { ...this.runIns, [id]: next };
    },

    /** @param {{ currentTarget: HTMLElement, detail: { value: string } }} event the picker's dropdown-change: nothing starts */
    runInPicked(event) {
      const id = event.currentTarget.dataset.serviceId;
      const prev = this.runIns[id];
      const picked = prev ? afterPick(prev, event.detail.value) : null;
      if (!picked) return;
      prefs.set(pickKeyOf(id), picked.clearStored ? null : event.detail.value);
      this.runIns = { ...this.runIns, [id]: picked.next };
      const w = pickedWorktree(picked.next);
      const name = this.services.find((s) => s.id === id)?.name ?? "";
      this.notice = `${name} runs in ${worktreeKindWord(w)}, branch ${branchLabel(w.branch)}.`;
    },

    /** The picker was touched to open (or Retry was pressed): the list is read again. */
    runInOpened(event) {
      return this.loadRunIn(event.currentTarget.dataset.serviceId);
    },

    dismissRunInFallback(event) {
      const id = event.currentTarget.dataset.serviceId;
      if (this.runIns[id]) this.runIns = { ...this.runIns, [id]: { ...this.runIns[id], fallback: null } };
    },

    /** Puts a picker's select on the pick once its options exist (x-effect on the select). */
    syncRunIn() {
      const el = this.$el;
      const pick = this.runIns[el.dataset.serviceId];
      if (!pick) return;
      const key = pick.pickedKey;
      void pick.worktrees.length;
      this.$nextTick(() => {
        if (el.isConnected && el.value !== key && [...el.options].some((o) => o.value === key)) el.value = key;
      });
    },

    /** @param {Event} event a row button's click: the row's service id is on the button */
    startRow(event) {
      return this.start(event.currentTarget.dataset.serviceId);
    },
    stopRow(event) {
      return this.stop(event.currentTarget.dataset.serviceId);
    },
    restartRow(event) {
      return this.restart(event.currentTarget.dataset.serviceId);
    },

    dismissRefusal() {
      this.refusal = null;
    },

    /** @param {KeyboardEvent & { currentTarget: HTMLElement }} event a key in the form drawer or the delete dialog */
    trap(event) {
      trapTab(event);
    },
    /** Esc closes the dialog on top: the delete question, else the form. */
    escape() {
      if (this.del !== null) this.cancelDelete();
      else if (this.formMode !== "") this.closeForm();
    },

    openNew() {
      this.opener = document.activeElement;
      this.formMode = "new";
      this.formId = "";
    },
    openEdit(id) {
      this.opener = document.activeElement;
      this.formMode = "edit";
      this.formId = id;
    },
    closeForm() {
      this.formMode = "";
      this.formId = "";
      this.restoreFocus();
    },
    /** Back to the control that opened the dialog; when it is gone (a menu item), to the page's New service. */
    restoreFocus() {
      this.$nextTick(() => {
        const back = this.opener?.isConnected ? this.opener : this.$el.querySelector("[data-new-service]");
        back?.focus?.();
      });
    },

    /** The form saved: the service joins the list now (its feed event follows and agrees). */
    saved(event) {
      const { service, restartNeeded, created } = event.detail;
      this.services = upsertService(this.services, service);
      this.readRunIns();
      this.closeForm();
      this.notice = savedNotice(service.name, { created, restartNeeded });
    },

    /** @param {{ currentTarget: HTMLElement, detail: { action: string } }} event the row menu's choice */
    menuSelect(event) {
      const id = event.currentTarget.dataset.serviceId;
      if (event.detail.action === "edit") this.openEdit(id);
      else if (event.detail.action === "delete") this.askDelete(id);
    },

    /** The delete question opens on Keep it, the safe answer. */
    focusKeep(el) {
      this.$nextTick(() => el.querySelector("[data-delete-keep]")?.focus());
    },

    askDelete(id) {
      const service = this.services.find((s) => s.id === id);
      if (!service || isActive(service)) return;
      this.opener = document.activeElement;
      this.del = { id, name: service.name, error: null };
    },
    cancelDelete() {
      if (this.deleting) return;
      this.del = null;
      this.restoreFocus();
    },
    async confirmDelete() {
      if (!this.del || this.deleting) return;
      const { id, name } = this.del;
      this.deleting = true;
      try {
        await gateway.deleteService(id);
        this.services = this.services.filter((s) => s.id !== id);
        if (this.logId === id) this.closeLog();
        this.del = null;
        this.notice = `${name} was deleted.`;
      } catch (err) {
        this.del = { id, name, error: describeError(err) };
      } finally {
        this.deleting = false;
      }
    },

    openLog(id) {
      this.logId = id;
    },
    closeLog() {
      this.logId = "";
    },
    /** @param {{ currentTarget: HTMLElement }} event the row's Log or Open log: the service id is on it */
    openLogRow(event) {
      this.openLog(event.currentTarget.dataset.serviceId);
    },

    retry() {
      return this.load();
    },

    dismissError() {
      this.error = null;
    },
  };
};
