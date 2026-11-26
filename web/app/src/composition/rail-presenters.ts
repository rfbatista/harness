// Builds the rail's model and presenter from the island props and the adapters
// a composition root chose. The server root and the browser root differ only in
// the adapters they pass.

import { Rail } from "../features/rail/application/rail";
import type { Announcer, FeaturePrefs, PageOpener, RailFeed, RailPrefs, RailSource, Scheduler, ServicesCount } from "../features/rail/application/ports";
import type { ProjectNav } from "../features/rail/domain/nav";
import { toSeed } from "../features/rail/infrastructure/rail-mapper";
import { RailPresenter } from "../features/rail/presentation/presenters/rail-presenter";
import { RailPropsDto, type RailNavDto } from "./rail-props-dto";

export interface RailAdapters {
  readonly source: RailSource;
  readonly feed: RailFeed;
  readonly prefs: RailPrefs;
  readonly features: FeaturePrefs;
  readonly announcer: Announcer;
  readonly opener: PageOpener;
  readonly scheduler: Scheduler;
  /** The services feed, for the Services item's badge; absent where nothing is followed (the server render). */
  readonly services?: ServicesCount;
}

export function toNav(dto: RailNavDto): ProjectNav {
  return {
    newTaskHref: dto.new_task_href,
    boardHref: dto.board_href,
    codeHref: dto.code_href,
    servicesHref: dto.services_href,
    servicesProjectId: dto.services_project_id,
    reviewsHref: dto.reviews_href,
    documentsHref: dto.documents_href,
    designHref: dto.design_href,
    designSystemHref: dto.design_system_href,
    designSystemProposals: dto.design_system_proposals,
    usageHref: dto.usage_href,
    productOwnerHref: dto.product_owner_href,
  };
}

export interface RailModel {
  readonly presenter: RailPresenter;
  /** Stops following the project feed and drops the timers. */
  readonly stop: () => void;
}

/** `props` is the rail island's props as the server sends them. Throws a DtoError when they cannot be read. */
export function buildRail(props: unknown, adapters: RailAdapters): RailModel {
  const dto = RailPropsDto.fromJSON(props);
  // The services feed is followed only where the project serves a Services page.
  const { services, ...rest } = adapters;
  const rail = new Rail({ ...rest, seed: toSeed(dto.seed), currentTaskId: dto.current_task, page: dto.page, collapsed: dto.collapsed, servicesRunning: dto.nav.services_running, ...(dto.nav.services_href !== "" && services ? { services } : {}) });
  const presenter = new RailPresenter(rail, adapters.features, { projectName: dto.project_name, nav: toNav(dto.nav) });
  return { presenter, stop: presenter.stop };
}
