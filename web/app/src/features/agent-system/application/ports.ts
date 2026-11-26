// What the Agent system page needs from the harness: the project's repositories, each scope's settings, and a save.

import type { Patch, Repository, Scope, SettingsView } from "../domain/settings";

export interface SettingsSource {
  /** The project's repositories, for the scope switch. */
  repositories(projectId: string, signal?: AbortSignal): Promise<Repository[]>;
  /** Project defaults, or one repository's effective configuration and overrides. Fails with a SettingsError. */
  view(projectId: string, scope: Scope, signal?: AbortSignal): Promise<SettingsView>;
  /** Sets or resets (null) fields at one level and answers that level's new view. Fails with SettingsRefused (422) or a SettingsError. */
  save(projectId: string, scope: Scope, patch: Patch): Promise<SettingsView>;
}
