// Builds the top bar's presenters from the island props and the adapters a
// composition root chose. The server root and the browser root differ only in
// the adapters they pass.

import { TopBarDto } from "../features/navigation/infrastructure/dto/top-bar-dto";
import { toTopBar } from "../features/navigation/infrastructure/top-bar-mapper";
import { CurrentTopBar } from "../features/navigation/application/current-top-bar";
import type { PageOpener } from "../features/navigation/application/ports";
import { PageLinksPresenter } from "../features/navigation/presentation/presenters/page-links-presenter";
import { ProjectPickerPresenter } from "../features/navigation/presentation/presenters/project-picker-presenter";
import { Inbox } from "../features/notifications/application/inbox";
import type { Navigator, NotificationsSource } from "../features/notifications/application/ports";
import { toBellProps } from "../features/notifications/infrastructure/bell-props";
import { BellPresenter } from "../features/notifications/presentation/presenters/bell-presenter";
import { PanelPresenter } from "../features/notifications/presentation/presenters/panel-presenter";
import { systemClock } from "../shared/kernel/clock";
import type { TopBarPresenters } from "./top-bar-view";

export interface TopBarAdapters {
  readonly pageOpener: PageOpener;
  readonly navigator: Navigator;
  readonly notifications: NotificationsSource;
}

export interface TopBarModel {
  readonly current: CurrentTopBar;
  readonly inbox: Inbox | null;
  readonly presenters: TopBarPresenters;
}

/** `props` is the top-bar model as the server sends it. Throws when its navigation part cannot be read. */
export function buildTopBar(props: unknown, adapters: TopBarAdapters): TopBarModel {
  const current = new CurrentTopBar(toTopBar(TopBarDto.fromJSON(props)));
  const bellProps = toBellProps((props as { notifications?: unknown } | null)?.notifications);
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
  return {
    current,
    inbox,
    presenters: { picker: new ProjectPickerPresenter(current, adapters.pageOpener), links: new PageLinksPresenter(current), bell },
  };
}
