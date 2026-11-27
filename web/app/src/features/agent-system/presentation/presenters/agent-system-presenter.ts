// The Agent system page: the scope switch, the layers' ledger rows, and a save per row.

import { systemClock, type Clock } from "../../../../shared/kernel/clock";
import { Presenter } from "../../../../shared/kernel/presenter";
import type { SettingsFeed, SettingsSource } from "../../application/ports";
import type { ScopeAddress } from "../../application/scope-address";
import { editables, gateLabel, layers, newVisualQa, NO_CONTEXT, type Edits, type Layer, type LayerContext } from "../../domain/ledger";
import { isNewProject, overrideEntries, redEntries, type RepositoryState } from "../../domain/overview";
import { byRow } from "../../domain/refusal";
import { gatePath, isRed, PROJECT_SCOPE, sameScope, sameValue, SettingsError, SettingsRefused, type FieldError, type LastScore, type ModelChoice, type Patch, type Repository, type RunCommand, type Scope, type SettingsView, type VisualQaUi } from "../../domain/settings";
import { check } from "../../domain/validation";

export interface RepositoryChoice extends Repository {
  /** A required gate has no command at any level. */
  readonly red: boolean;
}

/** One error in the save bar: where it is (section, row) and what it says. */
export interface ErrorEntry extends FieldError {
  readonly section: string;
  readonly label: string;
}

/** A required gate with no command at any level, for the red banner of a repository. */
export interface MissingGate {
  readonly kind: string;
  readonly label: string;
  /** Why detection found none ("gitleaks not installed"); "" when it did not say. */
  readonly reason: string;
}

/** A repository in the project-wide overview, with what makes it listed. */
export interface OverviewEntry {
  readonly repositoryId: string;
  readonly name: string;
  /** The field paths it overrides (Overrides); empty in Red repositories. */
  readonly fields: readonly string[];
  /** The required gates it misses, in words (Red repositories); empty in Overrides. */
  readonly missing: readonly string[];
}

/** The project's repositories at a glance: what they override, which are red, and whether nothing is saved at all. */
export interface Overview {
  readonly overrides: readonly OverviewEntry[];
  readonly red: readonly OverviewEntry[];
  /** Nothing is saved at any level: every value is a harness default, detected per repository. */
  readonly newProject: boolean;
}

export interface AgentSystemViewModel {
  readonly status: "loading" | "ready" | "failed";
  readonly error: string;
  readonly projectId: string;
  readonly projectName: string;
  readonly scope: Scope;
  /** "Project defaults" or the repository's name. */
  readonly scopeName: string;
  readonly repositories: readonly RepositoryChoice[];
  readonly layers: readonly Layer[];
  /** Required gates with no command at any level, at a repository's scope. */
  readonly missingGates: readonly MissingGate[];
  /** The shown repository's run commands: what a visual QA start command can be. Empty at project scope. */
  readonly runCommands: readonly RunCommand[];
  /** The repository's Instruction files, for the skills line; "" at project scope. */
  readonly skillsHref: string;
  readonly overview: Overview;
  /** The settings were saved elsewhere while this page is open (feed: agent_system_settings); Reload brings them in. */
  readonly changedElsewhere: boolean;
  /** "Saved · applies to sessions started afterwards" after a save, "" before. */
  readonly saved: string;
  /** The repository's scope offers Import from check.sh on its Gates sub-list. */
  readonly canImportCheckSh: boolean;
  /** What the last import did ("Imported 2 gates from check.sh. Review them, then Save."), or why it failed; "" before one. */
  readonly importNote: string;
  /** Why the last save failed for a reason that is not a field, with its code; "" when it did not. */
  readonly problem: string;
  /** The errors of the last refused save (422), until the person changes the rows they name. */
  readonly refused: readonly FieldError[];
  /** Rows changed and not saved. */
  readonly unsaved: number;
  /** Every error standing on a row, from the form or from a refused save, by section and field. */
  readonly errors: readonly ErrorEntry[];
  readonly saving: boolean;
  /** There is something to save and nothing stands in its way. */
  readonly canSave: boolean;
  /** "Save project defaults" / "Save overrides for <repository>". */
  readonly saveLabel: string;
}

export interface AgentSystemDeps {
  readonly source: SettingsSource;
  readonly address: ScopeAddress;
  /** Follows the saves other pages make; without it the page does not hear of them. */
  readonly feed?: SettingsFeed;
  readonly projectId: string;
  readonly projectName: string;
  /** A repository's Instruction files page. */
  readonly architectureHref: (repositoryId: string) => string;
  /** The project's Score page, for the Score section's line; "" leaves the line out. */
  readonly scoreHref?: string;
  /** The models the harness can call, for the critic model's select; empty leaves the check to the store. */
  readonly models?: readonly ModelChoice[];
  /** The time of the saved line's age; the system clock when absent. */
  readonly clock?: Clock;
}

const APPLIES = "applies to sessions started afterwards";

/** "Saved just now", "Saved 2 m ago", "Saved 3 h ago", then what the save means. */
function savedLine(ageMs: number): string {
  const minutes = Math.floor(Math.max(0, ageMs) / 60_000);
  const age = minutes < 1 ? "just now" : minutes < 60 ? `${minutes} m ago` : `${Math.floor(minutes / 60)} h ago`;
  return `Saved ${age} · ${APPLIES}`;
}

export class AgentSystemPresenter extends Presenter<AgentSystemViewModel> {
  readonly #deps: AgentSystemDeps;
  /** Each load takes a number; an answer for an older one is dropped. */
  #loading = 0;
  #project: SettingsView | undefined;
  /** The project level as the rows hold it, at either scope. */
  #projectView: SettingsView | undefined;
  /** The project level as the store holds it now: the overview reads it. */
  #liveProject: SettingsView | undefined;
  /** Every repository's level as the store holds it now. */
  readonly #states = new Map<string, SettingsView>();
  /** What the store held when it last differed from the rows: Reload brings it in. */
  #fresh: { readonly project: SettingsView; readonly view: SettingsView } | undefined;
  /** A change arrived while a save ran: it is read when the save ends. */
  #recheck = false;
  /** When the last save was answered: the saved line counts its age from it. */
  #savedAt = 0;
  #pending: Promise<void> = Promise.resolve();
  #stopFeed: (() => void) | undefined;
  #view: SettingsView | undefined;
  #runCommands: readonly RunCommand[] = [];
  #lastScore: LastScore | undefined;
  /** What the person changed and has not saved, by field path (null: reset); it belongs to the shown scope. */
  #draft: Record<string, unknown> = {};
  /** Drafted rows the person made their own with Override: an override equal to the inherited value stays one until Reset. */
  readonly #kept = new Set<string>();
  /** The store's refusals by field path, until the row changes. */
  #refusals: Record<string, FieldError> = {};
  #stopFollowing: (() => void) | undefined;

  constructor(deps: AgentSystemDeps) {
    super({ status: "loading", error: "", projectId: deps.projectId, projectName: deps.projectName, scope: PROJECT_SCOPE, scopeName: "Project defaults", repositories: [], layers: [], missingGates: [], runCommands: [], skillsHref: "", overview: { overrides: [], red: [], newProject: false }, changedElsewhere: false, saved: "", problem: "", refused: [], canImportCheckSh: false, importNote: "", unsaved: 0, errors: [], saving: false, canSave: false, saveLabel: "Save project defaults" });
    this.#deps = deps;
  }

  /** Reads the repositories and the scope the address names, then every repository's state for the red dots. */
  async start(): Promise<void> {
    this.#stopFollowing ??= this.#deps.address.follow((id) => void this.#load(id));
    this.#stopFeed ??= this.#deps.feed?.follow(this.#deps.projectId, { onChange: () => this.#changed() });
    this.update((vm) => ({ ...vm, status: "loading", error: "" }));
    let repositories: Repository[];
    try {
      repositories = await this.#deps.source.repositories(this.#deps.projectId);
    } catch (err) {
      this.#fail(err);
      return;
    }
    this.update((vm) => ({ ...vm, repositories: repositories.map((r) => ({ ...r, red: false })) }));
    await this.#load(this.#deps.address.read());
    await this.#readStates(repositories);
    await this.#readLastScore();
  }

  /** Resolves when the changes heard on the feed have been read. */
  settled(): Promise<void> {
    return this.#pending;
  }

  /** Keeps the saved line's age current; the page calls it on a timer. */
  tick(): void {
    if (this.viewModel.saved === "") return;
    this.#show({ saved: savedLine(this.#now() - this.#savedAt) });
  }

  #now(): number {
    return (this.#deps.clock ?? systemClock).now();
  }

  stop(): void {
    this.#stopFollowing?.();
    this.#stopFollowing = undefined;
    this.#stopFeed?.();
    this.#stopFeed = undefined;
    this.#loading++;
  }

  /** Switches the scope ("" is the project defaults) and keeps it in the address. */
  async selectScope(repositoryId: string): Promise<void> {
    this.#deps.address.write(repositoryId);
    await this.#load(repositoryId);
  }

  /** Drafts a row's value at the shown scope; a value equal to what the level holds is no change. */
  edit(path: string, value: unknown): void {
    const draft = { ...this.#draft };
    // Toggling a drafted row back to the value it was saved with is no change either.
    const saved = this.#rows().find((r) => r.path === path);
    const undone = path in draft && !this.#kept.has(path) && value !== null && saved !== undefined && sameValue(saved.current, value) && !(path in (this.#view?.values ?? {}));
    if (this.#holds(path, value) || undone) delete draft[path];
    else draft[path] = value;
    this.#draft = draft;
    const refusals = { ...this.#refusals };
    delete refusals[path];
    this.#refusals = refusals;
    this.#show({ saved: "", problem: "" });
  }

  /** Makes an inherited row this scope's own, keeping its value. */
  overrideHere(path: string): void {
    const row = this.#rows().find((r) => r.path === path);
    if (row === undefined) return;
    this.edit(path, row.current);
    if (path in this.#draft) this.#kept.add(path);
  }

  /** Drafts a number row from what the person typed: a whole number, or the text itself for the form to refuse. */
  editNumber(path: string, text: string): void {
    this.edit(path, /^\s*-?\d+\s*$/.test(text) ? Number(text) : text);
  }

  /** Makes an inherited gate this scope's own: its command and timeout as they read now. */
  overrideGate(kind: string): void {
    for (const path of [`guard.gates.${kind}.command`, `guard.gates.${kind}.timeout_seconds`]) this.overrideHere(path);
  }

  /** Drafts removing this gate at this level: every field of it the level sets (or the person set). */
  resetGate(kind: string): void {
    for (const attribute of ["command", "timeout_seconds", "enabled"]) {
      const path = `guard.gates.${kind}.${attribute}`;
      if (path in this.#draft || path in (this.#view?.values ?? {})) this.edit(path, null);
    }
  }

  /** Drafts removing exactly that field at this level. */
  reset(path: string): void {
    this.edit(path, null);
  }

  /** Chooses the critic's model; "" is the review-mode model, which resets the field at this level. */
  selectModel(model: string): void {
    this.edit("review.critic.model", model === "" ? null : model);
  }

  /** Starts a UI declaration for the shown repository, from its first run command. */
  declareVisualQa(): void {
    this.edit("review.visual_qa.ui", newVisualQa(this.#runCommands[0]?.command ?? ""));
  }

  /** Changes parts of the declaration; it is saved and refused as a whole. */
  editVisualQa(change: Partial<VisualQaUi>): void {
    const block = this.viewModel.layers.find((l) => l.key === "review")?.visualQa;
    if (block === undefined || !block.declared) return;
    this.edit("review.visual_qa.ui", { ...block.ui, ...change });
  }

  /** Removes the declaration as a whole: resets the field at this level. */
  removeVisualQa(): void {
    this.edit("review.visual_qa.ui", null);
  }

  /** Fills the draft with the gate commands the repository's check.sh would give, as overrides to review; saves nothing. */
  async importFromCheckSh(): Promise<void> {
    const scope = this.viewModel.scope;
    if (scope.kind !== "repository") return;
    try {
      const gates = await this.#deps.source.importFromCheckSh(this.#deps.projectId, scope.id);
      if (!sameScope(this.viewModel.scope, scope)) return;
      for (const gate of gates) this.edit(gatePath(gate.kind, "command"), gate.command);
      const note = gates.length === 0 ? "This repository's check.sh names no gate, or it has none: nothing to import." : `Imported ${gates.length} ${gates.length === 1 ? "gate" : "gates"} from check.sh. Review them, then Save.`;
      this.#show({ importNote: note });
    } catch (err) {
      if (!sameScope(this.viewModel.scope, scope)) return;
      this.#show({ importNote: err instanceof SettingsError ? `Import from check.sh failed. ${err.code}: ${err.message}` : String(err) });
    }
  }

  /** Throws the draft away. */
  discard(): void {
    this.#draft = {};
    this.#refusals = {};
    this.#show({ saved: "", problem: "", importNote: "" });
  }

  /** Sends one PATCH with every drafted path. A refusal keeps the draft and lands its errors on the rows. */
  async save(): Promise<void> {
    if (!this.viewModel.canSave || this.viewModel.saving) return;
    const scope = this.viewModel.scope;
    const patch: Patch = { ...this.#draft };
    this.update((vm) => ({ ...vm, saved: "", problem: "", saving: true }));
    try {
      const view = await this.#deps.source.save(this.#deps.projectId, scope, patch);
      if (!sameScope(this.viewModel.scope, scope)) return this.#staleSave(scope, view);
      // What the person typed while the save ran stays a draft.
      const draft: Record<string, unknown> = {};
      for (const [path, value] of Object.entries(this.#draft)) if (!(path in patch) || patch[path] !== value) draft[path] = value;
      this.#draft = draft;
      this.#refusals = {};
      this.#view = view;
      if (scope.kind === "project") {
        this.#project = undefined;
        this.#projectView = view;
        this.#liveProject = view;
      } else this.#states.set(scope.id, view);
      this.#fresh = undefined;
      this.#savedAt = this.#now();
      this.#show({ saved: savedLine(0), saving: false, changedElsewhere: false });
      // A project default may change what a repository inherits: its red dot and rows read it again.
      if (scope.kind === "project") await this.#readStates(this.viewModel.repositories);
      if (this.#recheck) {
        this.#recheck = false;
        await this.#refresh();
      }
    } catch (err) {
      if (!sameScope(this.viewModel.scope, scope)) return this.#staleSave(scope, undefined);
      if (err instanceof SettingsRefused) {
        this.#refusals = byRow(err.errors, this.#rows().map((r) => r.path));
        this.#show({ saving: false });
      } else {
        const problem = err instanceof SettingsError ? `${err.code}: ${err.message}` : String(err);
        this.#show({ problem, saving: false });
      }
    }
  }

  /** A save answered after the person moved to another scope: the page is not saving any more, and what it says is not applied to the scope now shown. */
  #staleSave(scope: Scope, view: SettingsView | undefined): void {
    if (view !== undefined) {
      // The store took it: what that level holds is what the caches say, as after a save shown.
      if (scope.kind === "project") {
        this.#project = undefined;
        this.#projectView = view;
        this.#liveProject = view;
      } else this.#states.set(scope.id, view);
    }
    this.update((vm) => ({ ...vm, saving: false }));
    this.#show({});
  }

  async #load(repositoryId: string): Promise<void> {
    const token = ++this.#loading;
    const known = this.viewModel.repositories.find((r) => r.id === repositoryId);
    const scope: Scope = known === undefined ? PROJECT_SCOPE : { kind: "repository", id: known.id };
    try {
      const project = await this.#deps.source.view(this.#deps.projectId, PROJECT_SCOPE);
      const view = scope.kind === "project" ? project : await this.#deps.source.view(this.#deps.projectId, scope);
      if (token !== this.#loading) return;
      const runCommands = scope.kind === "repository" ? await this.#readRunCommands(scope.id) : [];
      if (token !== this.#loading) return;
      this.#project = scope.kind === "project" ? undefined : project;
      this.#projectView = project;
      this.#liveProject = project;
      this.#fresh = undefined;
      this.#view = view;
      this.#runCommands = runCommands;
      this.#draft = {};
      this.#refusals = {};
      this.update((vm) => this.#shown(vm, scope, view, { status: "ready", error: "", saved: "", problem: "", importNote: "" }));
    } catch (err) {
      if (token === this.#loading) this.#fail(err);
    }
  }

  /** A repository's run commands; the page does without them when they cannot be read. */
  async #readRunCommands(repositoryId: string): Promise<RunCommand[]> {
    try {
      return await this.#deps.source.runCommands(repositoryId);
    } catch {
      return [];
    }
  }

  /** The Score section's line; a dash when the bench cannot be read. */
  async #readLastScore(): Promise<void> {
    try {
      this.#lastScore = await this.#deps.source.lastScore(this.#deps.projectId);
    } catch {
      this.#lastScore = undefined;
    }
    this.#show({});
  }

  #context(): LayerContext {
    return { ...NO_CONTEXT, models: this.#deps.models ?? [], lastScore: this.#lastScore, scoreHref: this.#deps.scoreHref ?? "" };
  }

  /** Reads every repository's level: the red dots and the overview follow them. */
  async #readStates(repositories: readonly Repository[]): Promise<void> {
    const token = this.#loading;
    const views = await Promise.all(repositories.map((r) => this.#deps.source.view(this.#deps.projectId, { kind: "repository", id: r.id }).catch(() => undefined)));
    if (token !== this.#loading) return;
    repositories.forEach((r, i) => {
      const view = views[i];
      if (view === undefined) this.#states.delete(r.id);
      else this.#states.set(r.id, view);
    });
    this.#redDots(repositories);
  }

  #redDots(repositories: readonly Repository[]): void {
    this.update((vm) => ({ ...vm, repositories: repositories.map((r) => ({ ...r, red: (() => { const v = this.#states.get(r.id); return v === undefined ? false : isRed(v); })() })) }));
    // The project's Guard index counts the red repositories.
    this.#show({});
  }

  /** A save was heard on the feed: the overview reads it, and the rows are told when it is not what they hold. */
  #changed(): void {
    this.#pending = this.#pending.then(() => this.#refresh());
  }

  async #refresh(): Promise<void> {
    if (this.viewModel.status !== "ready") return;
    // A save of the page's own is being answered: its echo is read once the answer is held.
    if (this.viewModel.saving) {
      this.#recheck = true;
      return;
    }
    const token = this.#loading;
    const scope = this.viewModel.scope;
    let project: SettingsView;
    try {
      project = await this.#deps.source.view(this.#deps.projectId, PROJECT_SCOPE);
    } catch {
      return;
    }
    if (token !== this.#loading) return;
    this.#liveProject = project;
    await this.#readStates(this.viewModel.repositories);
    if (token !== this.#loading || this.viewModel.saving) return;
    const shown = scope.kind === "project" ? project : this.#states.get(scope.id);
    const same = shown !== undefined && sameValue(shown, this.#view) && (scope.kind === "project" || sameValue(project, this.#projectView));
    this.#fresh = same || shown === undefined ? undefined : { project, view: shown };
    this.#show({ changedElsewhere: this.#fresh !== undefined });
  }

  /** Brings in what was saved elsewhere, keeping the rows the person changed and has not saved. */
  async reload(): Promise<void> {
    const fresh = this.#fresh;
    if (fresh === undefined) return;
    this.#fresh = undefined;
    this.#projectView = fresh.project;
    this.#project = this.viewModel.scope.kind === "project" ? undefined : fresh.project;
    this.#view = fresh.view;
    this.#show({ changedElsewhere: false, saved: "" });
  }

  #overview(): Overview {
    const project = this.#liveProject ?? this.#projectView;
    const repositories = this.viewModel.repositories;
    const states: RepositoryState[] = repositories.flatMap((r) => {
      const view = this.#states.get(r.id);
      return view === undefined ? [] : [{ repository: { id: r.id, name: r.name }, view }];
    });
    const loaded = project !== undefined && states.length === repositories.length;
    return {
      overrides: overrideEntries(states).map((e) => ({ repositoryId: e.repository.id, name: e.repository.name, fields: e.fields, missing: [] })),
      red: redEntries(states).map((e) => ({ repositoryId: e.repository.id, name: e.repository.name, fields: [], missing: e.missing.map(gateLabel) })),
      newProject: loaded && isNewProject(project, states),
    };
  }

  /** The fields as saved, without the draft: their rules say what a value may be. */
  #rows() {
    const view = this.#view;
    return view === undefined ? [] : editables(layers(this.viewModel.scope, view, this.#project, undefined, this.#context()));
  }

  /** The level already holds this value (a reset: it does not set the field). */
  #holds(path: string, value: unknown): boolean {
    const values = this.#view?.values ?? {};
    return value === null ? !(path in values) : path in values && sameValue(values[path], value);
  }

  /** Re-derives the page from the view, the draft and the errors. */
  #show(extra: Partial<AgentSystemViewModel>): void {
    for (const path of this.#kept) if (!(path in this.#draft)) this.#kept.delete(path);
    const view = this.#view;
    if (view === undefined) return;
    this.update((vm) => this.#shown(vm, vm.scope, view, extra));
  }

  #shown(vm: AgentSystemViewModel, scope: Scope, view: SettingsView, extra: Partial<AgentSystemViewModel>): AgentSystemViewModel {
    const repository = scope.kind === "repository" ? vm.repositories.find((r) => r.id === scope.id) : undefined;
    const project = scope.kind === "repository" ? this.#project : undefined;
    const known = new Map(editables(layers(scope, view, project, undefined, this.#context())).map((f) => [f.path, f] as const));
    const errors: Record<string, FieldError> = { ...this.#refusals };
    for (const [path, value] of Object.entries(this.#draft)) {
      const field = known.get(path);
      const error = field === undefined ? undefined : check(path, field.rule, value);
      if (error !== undefined) errors[path] = error;
    }
    const edits: Edits = { draft: this.#draft, errors };
    const redRepositories = vm.repositories.filter((r) => r.red).length;
    const shown = layers(scope, view, project, edits, this.#context()).map((l) => (l.key === "guard" && scope.kind === "project" ? { ...l, attention: redRepositories } : l));
    const listed: ErrorEntry[] = Object.values(errors).map((e) => ({ ...e, section: known.get(e.field)?.section ?? "", label: known.get(e.field)?.label ?? e.field }));
    const missingGates: MissingGate[] = scope.kind === "repository" ? (shown.find((l) => l.key === "guard")?.gates ?? []).filter((g) => g.missing).map((g) => ({ kind: g.kind, label: gateLabel(g.kind), reason: view.effective.guard.gates.find((x) => x.kind === g.kind)?.command.reason ?? "" })) : [];
    const unsaved = Object.keys(this.#draft).length;
    const refused = Object.values(this.#refusals);
    const saving = extra.saving ?? vm.saving;
    return {
      ...vm,
      scope,
      scopeName: repository?.name ?? "Project defaults",
      layers: shown,
      overview: this.#overview(),
      missingGates,
      runCommands: scope.kind === "repository" ? this.#runCommands : [],
      skillsHref: scope.kind === "repository" ? this.#deps.architectureHref(scope.id) : "",
      refused,
      canImportCheckSh: scope.kind === "repository",
      unsaved,
      errors: listed,
      canSave: unsaved > 0 && listed.length === 0 && !saving,
      saveLabel: repository === undefined ? "Save project defaults" : `Save overrides for ${repository.name}`,
      ...extra,
    };
  }

  #fail(err: unknown): void {
    const message = err instanceof SettingsError ? err.message : "The settings could not be read.";
    this.update((vm) => ({ ...vm, status: "failed", error: message }));
  }
}
