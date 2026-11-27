// The project-wide summary of its repositories: which fields each overrides and which are red.

import type { Repository, SettingsView } from "./settings";

/** One repository with its own level's view. */
export interface RepositoryState {
  readonly repository: Repository;
  readonly view: SettingsView;
}

export interface OverrideEntry {
  readonly repository: Repository;
  /** The field paths the repository sets itself. */
  readonly fields: readonly string[];
}

export interface RedEntry {
  readonly repository: Repository;
  /** The required gate kinds with no command at any level. */
  readonly missing: readonly string[];
}

/** The repositories that set at least one field themselves. */
export const overrideEntries = (states: readonly RepositoryState[]): OverrideEntry[] =>
  states.flatMap((s) => (Object.keys(s.view.values).length > 0 ? [{ repository: s.repository, fields: Object.keys(s.view.values) }] : []));

/** The repositories missing a required gate. */
export const redEntries = (states: readonly RepositoryState[]): RedEntry[] =>
  states.flatMap((s) => (s.view.effective.missingRequiredGates.length > 0 ? [{ repository: s.repository, missing: s.view.effective.missingRequiredGates }] : []));

/** Nothing is saved at any level: every value is a harness default. */
export const isNewProject = (project: SettingsView, states: readonly RepositoryState[]): boolean => Object.keys(project.values).length === 0 && states.every((s) => Object.keys(s.view.values).length === 0);
