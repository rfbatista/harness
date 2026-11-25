// The project picker: every project by name, the current one selected, and
// "Choose a project…" while no project is current. Not offered without projects.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { CurrentTopBar } from "../../application/current-top-bar";
import { openProject } from "../../application/open-project";
import type { PageOpener } from "../../application/ports";
import type { TopBar } from "../../domain/top-bar";

export interface ProjectPickerViewModel {
  readonly visible: boolean;
  /** The picker is a GET form: this is its action. */
  readonly action: string;
  /** The id of the selected project; "" while the placeholder is. */
  readonly selected: string;
  readonly showPlaceholder: boolean;
  readonly placeholder: string;
  readonly options: readonly { readonly id: string; readonly name: string }[];
}

export const PLACEHOLDER = "Choose a project…";

export function projectPickerViewModel(topBar: TopBar): ProjectPickerViewModel {
  const current = topBar.projects.some((project) => project.id === topBar.currentProjectId) ? topBar.currentProjectId : "";
  return {
    visible: topBar.projects.length > 0,
    action: topBar.switchProjectHref,
    selected: current,
    showPlaceholder: current === "",
    placeholder: PLACEHOLDER,
    options: topBar.projects.map(({ id, name }) => ({ id, name })),
  };
}

export class ProjectPickerPresenter extends Presenter<ProjectPickerViewModel> {
  readonly #current: CurrentTopBar;
  readonly #opener: PageOpener;

  constructor(current: CurrentTopBar, opener: PageOpener) {
    super(projectPickerViewModel(current.get()));
    this.#current = current;
    this.#opener = opener;
    current.subscribe((topBar) => this.set(projectPickerViewModel(topBar)));
  }

  /** The person picked a project: it opens at once. */
  choose = (projectId: string): void => {
    openProject(this.#opener, this.#current.get(), projectId);
  };
}
