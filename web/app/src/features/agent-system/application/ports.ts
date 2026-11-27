// What the Agent system page needs from the harness: the project's repositories, each scope's settings, and a save.

import type { LastScore, Patch, Repository, RunCommand, Scope, SettingsView } from "../domain/settings";

export interface SettingsSource {
  /** The project's repositories, for the scope switch. */
  repositories(projectId: string, signal?: AbortSignal): Promise<Repository[]>;
  /** Project defaults, or one repository's effective configuration and overrides. Fails with a SettingsError. */
  view(projectId: string, scope: Scope, signal?: AbortSignal): Promise<SettingsView>;
  /** The commands a repository can run, for the visual QA start command; fails with a SettingsError. */
  runCommands(repositoryId: string, signal?: AbortSignal): Promise<RunCommand[]>;
  /** The project's last finished bench run, or undefined when it has none; fails with a SettingsError. */
  lastScore(projectId: string, signal?: AbortSignal): Promise<LastScore | undefined>;
  /** Sets or resets (null) fields at one level and answers that level's new view. Fails with SettingsRefused (422) or a SettingsError. */
  save(projectId: string, scope: Scope, patch: Patch): Promise<SettingsView>;
}
