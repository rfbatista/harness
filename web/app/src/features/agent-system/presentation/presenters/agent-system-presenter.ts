// The Agent system page: the scope switch, the layers' ledger rows, and a save per row.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { SettingsSource } from "../../application/ports";
import type { ScopeAddress } from "../../application/scope-address";
import { layers, type Layer } from "../../domain/ledger";
import { isRed, PROJECT_SCOPE, SettingsError, SettingsRefused, type Repository, type Scope, type SettingsView } from "../../domain/settings";

export interface RepositoryChoice extends Repository {
  /** A required gate has no command at any level. */
  readonly red: boolean;
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
  /** The repository's Instruction files, for the skills line; "" at project scope. */
  readonly skillsHref: string;
  /** "Saved · applies to sessions started afterwards" after a save, "" before. */
  readonly saved: string;
  /** Why the last save was refused or failed, with its code; "" when it was not. */
  readonly problem: string;
  /** Field paths with a save in flight. */
  readonly pending: readonly string[];
}

export interface AgentSystemDeps {
  readonly source: SettingsSource;
  readonly address: ScopeAddress;
  readonly projectId: string;
  readonly projectName: string;
  /** A repository's Instruction files page. */
  readonly architectureHref: (repositoryId: string) => string;
}

const SAVED = "Saved · applies to sessions started afterwards";

export class AgentSystemPresenter extends Presenter<AgentSystemViewModel> {
  readonly #deps: AgentSystemDeps;
  /** Each load takes a number; an answer for an older one is dropped. */
  #loading = 0;
  #project: SettingsView | undefined;
  #view: SettingsView | undefined;
  #stopFollowing: (() => void) | undefined;

  constructor(deps: AgentSystemDeps) {
    super({ status: "loading", error: "", projectId: deps.projectId, projectName: deps.projectName, scope: PROJECT_SCOPE, scopeName: "Project defaults", repositories: [], layers: [], skillsHref: "", saved: "", problem: "", pending: [] });
    this.#deps = deps;
  }

  /** Reads the repositories and the scope the address names, then every repository's state for the red dots. */
  async start(): Promise<void> {
    this.#stopFollowing ??= this.#deps.address.follow((id) => void this.#load(id));
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
    await this.#markRed(repositories);
  }

  stop(): void {
    this.#stopFollowing?.();
    this.#stopFollowing = undefined;
    this.#loading++;
  }

  /** Switches the scope ("" is the project defaults) and keeps it in the address. */
  async selectScope(repositoryId: string): Promise<void> {
    this.#deps.address.write(repositoryId);
    await this.#load(repositoryId);
  }

  /** Saves one toggle's value at the shown scope. */
  change(path: string, value: boolean): Promise<void> {
    return this.#save(path, value);
  }

  /** Makes an inherited row this scope's own, keeping its value. */
  overrideHere(path: string): Promise<void> {
    const row = this.viewModel.layers.flatMap((l) => l.rows).find((r) => r.path === path);
    return row === undefined ? Promise.resolve() : this.#save(path, row.value);
  }

  /** Removes exactly that field at this level. */
  reset(path: string): Promise<void> {
    return this.#save(path, null);
  }

  async #load(repositoryId: string): Promise<void> {
    const token = ++this.#loading;
    const known = this.viewModel.repositories.find((r) => r.id === repositoryId);
    const scope: Scope = known === undefined ? PROJECT_SCOPE : { kind: "repository", id: known.id };
    try {
      const project = await this.#deps.source.view(this.#deps.projectId, PROJECT_SCOPE);
      const view = scope.kind === "project" ? project : await this.#deps.source.view(this.#deps.projectId, scope);
      if (token !== this.#loading) return;
      this.#project = scope.kind === "project" ? undefined : project;
      this.#view = view;
      this.update((vm) => this.#shown(vm, scope, view, { status: "ready", error: "", saved: "", problem: "" }));
    } catch (err) {
      if (token === this.#loading) this.#fail(err);
    }
  }

  async #markRed(repositories: readonly Repository[]): Promise<void> {
    const token = this.#loading;
    const red = await Promise.all(repositories.map(async (r) => {
      try {
        return isRed(await this.#deps.source.view(this.#deps.projectId, { kind: "repository", id: r.id }));
      } catch {
        return false;
      }
    }));
    if (token !== this.#loading) return;
    this.update((vm) => ({ ...vm, repositories: vm.repositories.map((r, i) => ({ ...r, red: red[i] ?? false })) }));
  }

  async #save(path: string, value: unknown): Promise<void> {
    const scope = this.viewModel.scope;
    this.update((vm) => ({ ...vm, saved: "", problem: "", pending: [...vm.pending, path] }));
    try {
      const view = await this.#deps.source.save(this.#deps.projectId, scope, { [path]: value });
      if (scope.kind === "project") this.#project = undefined;
      this.#view = view;
      const current = this.viewModel.scope;
      if (current.kind === scope.kind && (current.kind === "project" || (scope.kind === "repository" && current.id === scope.id))) {
        this.update((vm) => this.#shown(vm, scope, view, { saved: SAVED, pending: vm.pending.filter((p) => p !== path) }));
      }
      // A project default may change what a repository inherits: its red dot and rows read it again.
      if (scope.kind === "project") await this.#markRed(this.viewModel.repositories);
    } catch (err) {
      const problem = err instanceof SettingsRefused ? err.errors.map((e) => `${e.code}: ${e.message} (${e.field})`).join("; ") : err instanceof SettingsError ? `${err.code}: ${err.message}` : String(err);
      this.update((vm) => ({ ...vm, problem, pending: vm.pending.filter((p) => p !== path) }));
    }
  }

  #shown(vm: AgentSystemViewModel, scope: Scope, view: SettingsView, extra: Partial<AgentSystemViewModel>): AgentSystemViewModel {
    const repository = scope.kind === "repository" ? vm.repositories.find((r) => r.id === scope.id) : undefined;
    return {
      ...vm,
      scope,
      scopeName: repository?.name ?? "Project defaults",
      layers: layers(scope, view, scope.kind === "repository" ? this.#project : undefined),
      skillsHref: scope.kind === "repository" ? this.#deps.architectureHref(scope.id) : "",
      ...extra,
    };
  }

  #fail(err: unknown): void {
    const message = err instanceof SettingsError ? err.message : "The settings could not be read.";
    this.update((vm) => ({ ...vm, status: "failed", error: message }));
  }
}
