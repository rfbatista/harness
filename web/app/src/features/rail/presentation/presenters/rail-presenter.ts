// The Tasks nav of the rail: what the view draws from the model, and the
// commands it sends back. No framework here (MVP): the view is passive.

import { Presenter } from "../../../../shared/kernel/presenter";
import { activityOf } from "../../application/follow-project";
import { isFeatureOpen, needsYouCount, reviewsCount, type Rail, type RailModelState } from "../../application/rail";
import type { FeaturePrefs } from "../../application/ports";
import { navItems, servicesBadge, type NavIcon, type ProjectNav } from "../../domain/nav";
import { railGroups } from "../../domain/rail-groups";
import { toGroup, type GroupViewModel } from "./rail-rows";

/** The page of the project's navigation, as the view draws it. */
export interface NavItemViewModel {
  readonly key: string;
  readonly href: string;
  readonly icon: NavIcon;
  readonly label: string;
  readonly current: boolean;
  /** The count in the badge; "" hides it. Services: running (99+); Review requests: waiting on the person. */
  readonly badge: string;
  /** The Design systems badge: how many proposals wait; its element is always drawn (the design-system page updates it). */
  readonly proposals: number | null;
  /** The Services item's running count (data-running-services); null for the other items. */
  readonly running: number | null;
}

export interface RailViewModel {
  readonly projectId: string;
  readonly projectName: string;
  /** The project's pages, left out when not served. */
  readonly nav: readonly NavItemViewModel[];
  readonly newTaskHref: string;
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

export interface RailStatics {
  readonly projectName: string;
  readonly nav: ProjectNav;
}

export function railViewModel(state: RailModelState, features: FeaturePrefs, statics: RailStatics): RailViewModel {
  const byTask = activityOf(state);
  const ctx = {
    projectId: state.projectId,
    byTask,
    currentId: state.currentTaskId,
    fresh: new Set(state.freshIds),
    signal: new Set(state.signalIds),
    focusedId: state.focusedId,
    query: state.query,
    open: (id: string) => isFeatureOpen(state, features, id),
  };
  // Done's rest is behind a closed disclosure: the server's paint leaves it out and the island adds it once it has taken over.
  const groups = railGroups(state.tasks, { byTask, currentId: state.currentTaskId, collapsed: state.collapsed, query: state.query, needsYouOnly: state.needsYouOnly }).map((g) => toGroup(g, ctx, state.prefsRead));
  const matched = groups.reduce((n, g) => n + g.rows.length, 0);
  const query = state.query.trim();
  const waiting = needsYouCount(state);
  const reviews = reviewsCount(state);
  return {
    projectId: state.projectId,
    projectName: statics.projectName,
    newTaskHref: statics.nav.newTaskHref,
    nav: navItems(statics.nav).map((i) => ({
      key: i.key,
      href: i.href,
      icon: i.icon,
      label: i.label,
      current: i.key === state.page,
      badge: i.key === "services" ? servicesBadge(state.servicesRunning) : i.key === "reviews" ? (reviews > 0 ? String(reviews) : "") : "",
      proposals: i.key === "design-system" ? statics.nav.designSystemProposals : null,
      running: i.key === "services" ? state.servicesRunning : null,
    })),
    isEmpty: state.tasks.length === 0,
    hasTasks: state.tasks.length > 0,
    total: state.tasks.filter((t) => !t.archivedAt).length,
    reviewsCount: reviews,
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

  constructor(rail: Rail, features: FeaturePrefs, statics: RailStatics) {
    super(railViewModel(rail.state, features, statics));
    this.#rail = rail;
    rail.subscribe((state) => this.set(railViewModel(state, features, statics)));
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
  /** The island has taken over the server's paint. */
  hydrated = (): void => this.#rail.readPrefs();
  setPage = (page: string): void => this.#rail.setPage(page);
  openDrawer = (): void => this.#rail.openDrawer();
  closeDrawer = (): void => this.#rail.closeDrawer();
  stop = (): void => this.#rail.stop();
}
