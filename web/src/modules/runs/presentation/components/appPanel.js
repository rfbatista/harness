// A session's App panel: run the repository's application from the session's
// worktree and watch it. Pick a saved run command or type one; the run's
// terminal (a run terminal, mounted per run) shows its output live, colors and
// all, and takes keys, so Ctrl+C reaches the app. A typed command can be saved
// for the repository under a name.
//
//   <template x-for="panel in appPanels" x-bind:key="panel.key">
//     <section x-data="runsAppPanel(panel)"> …
//       <template x-for="rid in runIds" x-bind:key="rid">
//         <div x-data="sessionsRunTerminal(rid)" x-on:terminal-exited="refresh"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { isRunning, preferredRun, runState } from "../../domain/run.js";

/** The choice that means "type a command". */
export const TYPED = "";

/** @param {{ gateway: import("../../domain/ports.js").RunGateway }} deps */
export const appPanel = ({ gateway }) => (panel = {}) => ({
  sessionId: panel.sessionId ?? "",
  repositoryId: panel.repositoryId ?? "",
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
  ready: false,
  busy: false,
  error: null,
  notice: "",

  async init() {
    try {
      const [commands, runs] = await Promise.all([
        gateway.listCommands(this.repositoryId),
        gateway.listRuns(this.sessionId),
      ]);
      this.commands = commands;
      this.runs = runs;
      if (commands.length > 0) this.choice = commands[0].name;
      this.runId = preferredRun(runs)?.id ?? "";
    } catch (err) {
      this.error = describeError(err);
    }
    this.ready = true;
  },

  // --- what the markup reads ---

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
    return this.runs.map((r) => ({ id: r.id, label: r.name, ...runState(r), current: r.id === this.runId }));
  },
  get hasHistory() {
    return this.runs.length > 1;
  },
  get showsEmpty() {
    return this.ready && !this.hasRun;
  },

  // --- actions ---

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
      const run = await gateway.start(this.sessionId, what);
      this.replace(run);
      this.runId = run.id;
    } catch (err) {
      this.error = describeError(err);
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
});
