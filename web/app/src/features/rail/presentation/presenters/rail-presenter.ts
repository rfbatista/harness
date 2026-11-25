// The Tasks nav of the rail: what the view draws from the model, and the
// commands it sends back. No framework here (MVP): the view is passive.

import { Presenter } from "../../../../shared/kernel/presenter";
import { activityOf } from "../../application/follow-project";
import { isFeatureOpen, needsYouCount, reviewsCount, type Rail, type RailModelState } from "../../application/rail";
import type { FeaturePrefs } from "../../application/ports";
import { railGroups } from "../../domain/rail-groups";
import { toGroup, type GroupViewModel } from "./rail-rows";

export interface RailViewModel {
  readonly projectId: string;
  /** The project has no task yet. */
  readonly isEmpty: boolean;
  readonly hasTasks: boolean;
  /** The heading's count: the tasks not archived. */
  readonly total: number;
  readonly reviewsCount: number;
  readonly needsYouCount: number;
  /** The toggle hides at 0, unless it is on: the view never switches mode by itself. */
  readonly showsNeedsYou: boolean;
  readonly needsYouOnly: boolean;
  readonly query: string;
  /** Read out by the polite region under the filter. */
  readonly filterResult: string;
  readonly hasNoMatch: boolean;
  readonly noMatchText: string;
  readonly nothingNeedsYou: boolean;
  readonly groups: readonly GroupViewModel[];
  /** The chevron turns only once a heading was clicked. */
  readonly turned: boolean;
  readonly drawerOpen: boolean;
}

export function railViewModel(state: RailModelState, features: FeaturePrefs): RailViewModel {
  const byTask = activityOf(state);
  const groups = railGroups(state.tasks, { byTask, currentId: state.currentTaskId, collapsed: state.collapsed, query: state.query, needsYouOnly: state.needsYouOnly }).map((g) =>
    toGroup(g, {
      projectId: state.projectId,
      byTask,
      currentId: state.currentTaskId,
      fresh: new Set(state.freshIds),
      signal: new Set(state.signalIds),
      focusedId: state.focusedId,
      query: state.query,
      open: (id) => isFeatureOpen(state, features, id),
    }),
  );
  const matched = groups.reduce((n, g) => n + g.rows.length, 0);
  const query = state.query.trim();
  const waiting = needsYouCount(state);
  return {
    projectId: state.projectId,
    isEmpty: state.tasks.length === 0,
    hasTasks: state.tasks.length > 0,
    total: state.tasks.filter((t) => !t.archivedAt).length,
    reviewsCount: reviewsCount(state),
    needsYouCount: waiting,
    showsNeedsYou: waiting > 0 || state.needsYouOnly,
    needsYouOnly: state.needsYouOnly,
    query: state.query,
    filterResult: state.filterResult,
    hasNoMatch: query !== "" && matched === 0,
    noMatchText: `No task matches “${query}”.`,
    nothingNeedsYou: state.needsYouOnly && query === "" && waiting === 0,
    groups,
    turned: state.turned,
    drawerOpen: state.drawerOpen,
  };
}

export class RailPresenter extends Presenter<RailViewModel> {
  readonly #rail: Rail;

  constructor(rail: Rail, features: FeaturePrefs) {
    super(railViewModel(rail.state, features));
    this.#rail = rail;
    rail.subscribe((state) => this.set(railViewModel(state, features)));
  }

  toggleGroup = (key: string): void => this.#rail.toggleGroup(key);
  toggleFeature = (id: string): void => this.#rail.toggleFeature(id);
  toggleNeedsYou = (): void => this.#rail.toggleNeedsYou();
  setQuery = (query: string): void => this.#rail.setQuery(query);
  clearQuery = (): void => this.#rail.clearQuery();
  /** Enter in the filter. False when there was nothing to open. */
  openFirstMatch = (): boolean => this.#rail.openFirstMatch();
  setFocused = (taskId: string): void => this.#rail.setFocused(taskId);
  clearFocusedRow = (): void => this.#rail.clearFocusedRow();
  setCurrentTask = (taskId: string): void => this.#rail.setCurrentTask(taskId);
  openDrawer = (): void => this.#rail.openDrawer();
  closeDrawer = (): void => this.#rail.closeDrawer();
  stop = (): void => this.#rail.stop();
}
