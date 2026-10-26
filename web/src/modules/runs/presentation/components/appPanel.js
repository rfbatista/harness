// A session's App panel: run the repository's application from the session's
// worktree and watch it. Pick a saved run command or type one (one Combobox
// with free text: saved commands are its suggestions); the run's
// terminal (a run terminal, mounted per run) shows its output live, colors and
// all, and takes keys, so Ctrl+C reaches the app. A typed command can be saved
// for the repository under a name.
//
//   <template x-for="panel in appPanels" x-bind:key="panel.key">
//     <section x-data="runsAppPanel(panel)"> …
//       <template x-for="rid in runIds" x-bind:key="rid">
//         <div x-data="sessionsRunTerminal(rid)" x-on:terminal-exited="refresh"> …

import { Codes, codeOf } from "../../../../shared/domain/errors.js";
import { describeError } from "../../../../shared/presentation/errors.js";
import {
  OWN_WORKTREE, branchLabel, isRunning, preferredRun, runState, sessionStatusWord,
  worktreeBadge, worktreeDescription, worktreeKindWord, worktreeTone,
} from "../../domain/run.js";

/** The choice that means "type a command". */
export const TYPED = "";

/** Where the pick is kept: per session, as the server's opaque key (ADR 0037). */
const pickKeyOf = (sessionId) => `app-run-worktree:${sessionId}`;

const noPrefs = { get: () => null, set() {} };

/**
 * @param {{
 *   gateway: import("../../domain/ports.js").RunGateway,
 *   prefs?: import("../../../../shared/infrastructure/storage.js").Preferences,  the browser's own choices
 * }} deps
 */
export const appPanel = ({ gateway, prefs = noPrefs }) => (panel = {}) => {
  /**
   * The panel's element, kept from init. The command field's select is not
   * in the panel's $refs: its x-ref belongs to the combobox's own x-data.
   */
  let root = null;
  const commandSelect = () => root?.querySelector('select[x-ref="command"]') ?? null;

  return {
    sessionId: panel.sessionId ?? "",
    repositoryId: panel.repositoryId ?? "",
    repositoryName: panel.repositoryName ?? "",
    /** The session's branch and worktree: where its runs take their code from. */
    branch: panel.branch ?? "",
    worktree: panel.worktree ?? "",
    /** @type {import("../../domain/run.js").RunCommand[]} */
    commands: [],
    /** @type {import("../../domain/run.js").Run[]} */
    runs: [],
    /** The run on screen. */
    runId: "",
    /** A saved command's name, or TYPED. */
    choice: TYPED,
    typed: "",
    saveName: "",
    /** @type {import("../../domain/run.js").RunWorktree[]} */
    worktrees: [],
    /** "loading" until the first list arrives; "failed" keeps Run working in the session's own worktree. */
    worktreesState: "loading",
    /** The picked run worktree's key. */
    pickedKey: OWN_WORKTREE,
    /** Whether the remembered pick has been looked at: it waits for the first list that arrives. */
    restored: false,
    /** Says the remembered or current pick is gone; null for no banner. */
    fallback: null,
    /** The pick, said once for screen readers. */
    announced: "",
    ready: false,
    busy: false,
    error: null,
    notice: "",

    async init() {
      root = this.$el;
      try {
        const [commands, runs] = await Promise.all([
          gateway.listCommands(this.repositoryId),
          gateway.listRuns(this.sessionId),
          this.loadWorktrees(),
        ]);
        this.commands = commands;
        this.runs = runs;
        if (commands.length > 0) this.choice = commands[0].name;
        this.showChoice();
        this.runId = preferredRun(runs)?.id ?? "";
      } catch (err) {
        this.error = describeError(err);
      }
      this.ready = true;
    },

    // --- what the markup reads ---

    /** Which repository's code a run uses, by name (its id when the name is unknown). */
    get repositoryLabel() {
      return this.repositoryName || this.repositoryId;
    },
    get hasBranch() {
      return this.branch !== "";
    },
    /** Where the run on screen runs; the session's worktree until there is one. */
    get runDir() {
      return this.run?.dir || this.worktree;
    },

    // --- the run worktree picker ---

    /** The picker's options, in the server's order. */
    get worktreeOptions() {
      return this.worktrees.map((w) => ({
        key: w.key,
        label: branchLabel(w.branch),
        description: worktreeDescription(w),
        tone: worktreeTone(w),
        session: w.kind === "session",
      }));
    },
    get ownAndMainOptions() {
      return this.worktreeOptions.filter((o) => !o.session);
    },
    get sessionOptions() {
      return this.worktreeOptions.filter((o) => o.session);
    },
    get sessionGroupLabel() {
      return `Other sessions in ${this.repositoryLabel}`;
    },
    /** The picked run worktree; the session's own until the list is in. */
    get pick() {
      return this.worktrees.find((w) => w.key === this.pickedKey) ?? null;
    },
    get pickIsMain() {
      return this.pick?.kind === "main";
    },
    get pickKind() {
      return this.pick?.kind ?? "own";
    },
    get pickBranch() {
      return this.pick ? this.pick.branch : this.branch;
    },
    get hasPickBranch() {
      return this.pickBranch !== "";
    },
    get pickPath() {
      return this.pick?.dir || this.worktree;
    },
    /** The "where the code comes from" line's ending, after the branch. */
    get pickTail() {
      switch (this.pickKind) {
        case "main":
          return "— the main checkout, your own working copy. No env files are written there.";
        case "session":
          return `— the worktree of the ${this.pick.agent || "no-agent"} session (${sessionStatusWord(this.pick.sessionStatus)}).`;
        default:
          return "— this session's worktree, with the agent's changes, not your own checkout.";
      }
    },
    get worktreesLoading() {
      return this.worktreesState === "loading" ? "true" : null;
    },
    get worktreesError() {
      return this.worktreesState === "failed" ? "Could not load worktrees." : null;
    },
    get hasFallback() {
      return this.fallback !== null;
    },
    /** What a start sends: nothing until a list is in, which the server reads as the session's own. */
    get startKey() {
      return this.worktreesState === "ready" ? this.pickedKey : "";
    },

    /** The run worktree a run names, with its agent when the list still has it. */
    worktreeOf(run) {
      const known = this.worktrees.find((w) => w.key === run.worktree.key);
      return known ?? { ...run.worktree, dir: run.dir, agent: "", sessionStatus: "" };
    },
    /** The badge in a run's header. */
    get runBadge() {
      return this.run ? worktreeBadge(this.worktreeOf(this.run)) : "";
    },
    get runBadgeIsMain() {
      return this.run?.worktree.kind === "main";
    },

    /** Reads the list: on load (the first list that arrives restores the remembered pick), when the picker opens, and after a refusal. */
    async loadWorktrees({ silent = false } = {}) {
      if (this.worktreesState !== "ready") this.worktreesState = "loading";
      const before = this.pick;
      try {
        this.worktrees = await gateway.listWorktrees(this.sessionId);
      } catch {
        // A list already read stays good enough: the pick must not silently turn into the session's own.
        if (!this.restored) this.worktreesState = "failed";
        return;
      }
      const restore = !this.restored;
      this.restored = true;
      this.worktreesState = "ready";
      this.reconcilePick({ restore, silent, before });
      this.showPick();
    },

    /** Puts the pick on a worktree that is on offer: the remembered one on load, the session's own when it is gone. */
    reconcilePick({ restore, silent, before }) {
      const offered = (key) => this.worktrees.some((w) => w.key === key);
      if (restore) {
        const stored = prefs.get(pickKeyOf(this.sessionId));
        if (stored === null || stored === OWN_WORKTREE) return;
        if (offered(stored)) {
          this.pickedKey = stored;
          return;
        }
        this.fall(null, silent);
        return;
      }
      if (offered(this.pickedKey)) return;
      this.fall(before, silent);
    },

    /** The pick is gone: back to the session's own worktree, said once. */
    fall(gone, silent) {
      this.pickedKey = OWN_WORKTREE;
      prefs.set(pickKeyOf(this.sessionId), null);
      if (silent) return;
      const what = gone?.kind === "session" && gone.agent ? `The worktree of ${gone.agent}${gone.branch ? ` on ${gone.branch}` : ""}` : "That worktree";
      this.fallback = { message: `${what} no longer exists. Running in this session's worktree.` };
    },

    dismissFallback() {
      this.fallback = null;
    },

    /** The picker's dropdown-change: a worktree is picked; nothing starts. */
    worktreePicked(e) {
      const key = e.detail.value;
      if (!this.worktrees.some((w) => w.key === key)) return;
      this.pickedKey = key;
      this.fallback = null;
      prefs.set(pickKeyOf(this.sessionId), key === OWN_WORKTREE ? null : key);
      const w = this.pick;
      this.announced = `Runs in ${worktreeKindWord(w)}, branch ${branchLabel(w.branch)}.`;
    },

    /** The picker was touched to open: read the list again. */
    reopened() {
      return this.loadWorktrees();
    },

    /** Puts the picker's select on the pick once its options exist. */
    showPick() {
      this.$nextTick(() => {
        const select = root?.querySelector("select#run-worktree");
        if (select) select.value = this.pickedKey;
      });
    },

    get typing() {
      return this.choice === TYPED;
    },
    get hasCommands() {
      return this.commands.length > 0;
    },
    get chosenCommand() {
      return this.commands.find((c) => c.name === this.choice)?.command ?? "";
    },
    get cannotRun() {
      return !this.ready || this.busy || (this.typing && !this.typed.trim());
    },
    get cannotSave() {
      return this.busy || !this.typed.trim() || !this.saveName.trim();
    },
    get canForget() {
      return !this.typing && !this.busy;
    },
    get run() {
      return this.runs.find((r) => r.id === this.runId) ?? null;
    },
    /** Tells the page which run is on screen ("" for none), for the tab row's terminal tools (x-effect). */
    reportRun() {
      this.$dispatch("run-shown", { id: this.runId });
    },
    /** The run on screen, as a one-item list keyed on its id, so another run mounts a fresh terminal. */
    get runIds() {
      return this.runId ? [this.runId] : [];
    },
    get hasRun() {
      return this.run !== null;
    },
    get running() {
      return this.run !== null && isRunning(this.run);
    },
    get runLabel() {
      return this.run?.name ?? "";
    },
    get runCommandLine() {
      return this.run && this.run.command !== this.run.name ? this.run.command : "";
    },
    get runStatusState() {
      return this.run ? runState(this.run).state : "idle";
    },
    get runStatusWord() {
      return this.run ? runState(this.run).word : "";
    },
    get history() {
      // A tag names where a run runs, only once the runs differ by worktree.
      const tagged = new Set(this.runs.map((r) => r.worktree.key)).size > 1;
      return this.runs.map((r) => ({
        id: r.id, label: r.name, tag: tagged ? worktreeKindWord(this.worktreeOf(r)) : "", ...runState(r), current: r.id === this.runId,
      }));
    },
    get hasHistory() {
      return this.runs.length > 1;
    },
    get showsEmpty() {
      return this.ready && !this.hasRun;
    },

    // --- actions ---

    /** The command field's dropdown-change: a saved command, or typed text (created). */
    commandPicked(e) {
      const { value, created } = e.detail;
      if (!created && this.commands.some((c) => c.name === value)) {
        this.choice = value;
        this.typed = "";
      } else {
        this.choice = TYPED;
        this.typed = value;
      }
    },

    /** Typing in the command field: the text is the command to run, as typed. */
    commandTyped(e) {
      if (!(e.target instanceof HTMLInputElement) || e.target.type === "hidden") return;
      this.choice = TYPED;
      this.typed = e.target.value;
    },

    /** Puts the command field on the chosen saved command once its option exists. */
    showChoice() {
      this.$nextTick(() => {
        const select = commandSelect();
        if (select && !this.typing) select.value = this.choice;
      });
    },

    async start() {
      if (this.cannotRun) return;
      const what = this.typing ? { command: this.typed } : { name: this.choice };
      await this.launch(what);
    },

    /** Runs the run on screen again: a stopped or ended one, or a running one after stopping it. */
    async restart() {
      const run = this.run;
      if (!run || this.busy) return;
      this.busy = true;
      this.error = null;
      try {
        if (isRunning(run)) this.replace(await gateway.stop(run.id));
      } catch (err) {
        this.error = describeError(err);
        this.busy = false;
        return;
      }
      this.busy = false;
      const saved = this.commands.some((c) => c.name === run.name);
      await this.launch(saved ? { name: run.name } : { command: run.command });
    },

    async stop() {
      if (!this.running || this.busy) return;
      this.busy = true;
      this.error = null;
      try {
        this.replace(await gateway.stop(this.runId));
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.busy = false;
      }
    },

    async launch(what) {
      this.busy = true;
      this.error = null;
      this.notice = "";
      try {
        const run = await gateway.start(this.sessionId, { ...what, worktree: this.startKey });
        const reopened = what.name && this.runs.some((r) => r.id === run.id && isRunning(r));
        this.replace(run);
        this.runId = run.id;
        if (reopened) this.notice = `“${what.name}” is already running in this worktree. Showing that run.`;
      } catch (err) {
        this.error = describeError(err);
        // The pick may be gone: read the list again, and say it once, as the refusal.
        if (codeOf(err) === Codes.WORKTREE_NOT_AVAILABLE) await this.loadWorktrees({ silent: true });
      } finally {
        this.busy = false;
      }
    },

    async save() {
      if (this.cannotSave) return;
      this.busy = true;
      this.error = null;
      try {
        const saved = await gateway.saveCommand(this.repositoryId, this.saveName, this.typed);
        this.commands = [...this.commands.filter((c) => c.name !== saved.name), saved].sort((a, b) => a.name.localeCompare(b.name));
        this.choice = saved.name;
        this.typed = "";
        this.saveName = "";
        this.showChoice();
        this.notice = `Saved “${saved.name}” for this repository.`;
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.busy = false;
      }
    },

    /** Removes the chosen saved command from the repository. */
    async forget() {
      if (!this.canForget) return;
      const name = this.choice;
      this.busy = true;
      this.error = null;
      try {
        await gateway.deleteCommand(this.repositoryId, name);
        this.commands = this.commands.filter((c) => c.name !== name);
        this.choice = this.commands[0]?.name ?? TYPED;
        this.showChoice();
        this.notice = `Removed “${name}”.`;
      } catch (err) {
        this.error = describeError(err);
      } finally {
        this.busy = false;
      }
    },

    /** Shows another run from this session's history. */
    show(id) {
      this.runId = id;
    },

    /** Re-reads the runs: after a run's terminal reports its exit. */
    async refresh() {
      try {
        this.runs = await gateway.listRuns(this.sessionId);
      } catch (err) {
        this.error = describeError(err);
      }
    },

    replace(run) {
      this.runs = [run, ...this.runs.filter((r) => r.id !== run.id)].sort((a, b) => b.startedAt - a.startedAt);
    },
  };
};
