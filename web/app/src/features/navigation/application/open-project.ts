import type { TopBar } from "../domain/top-bar";
import type { PageOpener } from "./ports";

/** Opens a project through the picker's GET form: the switch href with `project=<id>`. */
export function openProject(opener: PageOpener, topBar: TopBar, projectId: string): void {
  if (!topBar.projects.some((project) => project.id === projectId)) return;
  opener.open(`${topBar.switchProjectHref}?${new URLSearchParams({ project: projectId })}`);
}
