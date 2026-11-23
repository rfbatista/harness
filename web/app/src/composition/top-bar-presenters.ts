// Builds the top bar's presenters from the island props and the adapters a
// composition root chose. The server root and the browser root differ only in
// the adapters they pass.

import { toTopBar } from "../features/navigation/infrastructure/top-bar-mapper";
import { CurrentTopBar } from "../features/navigation/application/current-top-bar";
import type { PageOpener } from "../features/navigation/application/ports";
import { PageLinksPresenter } from "../features/navigation/presentation/presenters/page-links-presenter";
import { ProjectPickerPresenter } from "../features/navigation/presentation/presenters/project-picker-presenter";
import { Inbox } from "../features/notifications/application/inbox";
import type { Navigator, NotificationsSource } from "../features/notifications/application/ports";
import { toBellProps } from "../features/notifications/infrastructure/bell-props";
import { TopBarPropsDto } from "./top-bar-props-dto";
import { BellPresenter } from "../features/notifications/presentation/presenters/bell-presenter";
import { PanelPresenter } from "../features/notifications/presentation/presenters/panel-presenter";
import type { OwnerDock } from "../features/product-owner/application/owner-dock";
import { TogglePresenter } from "../features/product-owner/presentation/presenters/toggle-presenter";
import { systemClock } from "../shared/kernel/clock";
import type { TopBarPresenters } from "./top-bar-view";

export interface TopBarAdapters {
  readonly pageOpener: PageOpener;
  readonly navigator: Navigator;
  readonly notifications: NotificationsSource;
  /** The document's product-owner dock: the toggle and the panel share it. */
  readonly dock: OwnerDock;
}

export interface TopBarModel {
  readonly current: CurrentTopBar;
  readonly inbox: Inbox | null;
  /** Releases the dock from this top bar's project. */
  readonly release: () => void;
  readonly presenters: TopBarPresenters;
}

/** `props` is the top-bar model as the server sends it. Throws a DtoError when its navigation part cannot be read. */
export function buildTopBar(props: unknown, adapters: TopBarAdapters): TopBarModel {
  const dto = TopBarPropsDto.fromJSON(props);
  const current = new CurrentTopBar(toTopBar(dto.topBar));
  const bellProps = dto.notifications && toBellProps(dto.notifications);
  let inbox: Inbox | null = null;
  let bell: TopBarPresenters["bell"] = null;
  if (bellProps) {
    inbox = new Inbox(adapters.notifications, { navigator: adapters.navigator });
    bell = {
      presenter: new BellPresenter(inbox, bellProps.inboxHref),
      panel: new PanelPresenter(inbox, adapters.notifications, systemClock, bellProps.inboxHref, bellProps.page),
    };
    inbox.start(bellProps.page);
  }
  // The dock follows the project the top bar is on: another frame brings a new top bar with its own project.
  let bound: string | null = null;
  let release = (): void => {};
  const bindDock = (projectId: string): void => {
    if (projectId === bound) return;
    release();
    bound = projectId;
    release = projectId === "" ? () => {} : adapters.dock.bind(projectId);
  };
  const stopFollowing = current.subscribe((topBar) => bindDock(topBar.currentProjectId));
  return {
    current,
    inbox,
    release: () => {
      stopFollowing();
      release();
    },
    presenters: { picker: new ProjectPickerPresenter(current, adapters.pageOpener), owner: new TogglePresenter(adapters.dock), links: new PageLinksPresenter(current), bell },
  };
}
