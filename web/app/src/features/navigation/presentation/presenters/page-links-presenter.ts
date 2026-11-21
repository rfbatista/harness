// The links to the app-wide pages the server serves, in the server's order,
// with the current page marked and the attention mark where something blocks.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { CurrentTopBar } from "../../application/current-top-bar";
import { LINK_PRESENTATION, type IconName, type LinkKey, type TopBar } from "../../domain/top-bar";

export interface LinkViewModel {
  readonly key: LinkKey;
  readonly href: string;
  readonly label: string;
  /** Present on icon links, which carry the label as accessible name and tooltip. */
  readonly icon?: IconName;
  /** A note that describes the link (tooltip and screen readers). */
  readonly note?: string;
  readonly current: boolean;
  readonly attention: boolean;
}

export interface PageLinksViewModel {
  readonly links: readonly LinkViewModel[];
}

export function pageLinksViewModel(topBar: TopBar): PageLinksViewModel {
  return {
    links: topBar.links.map(({ key, href, current, attention }) => {
      const { label, icon, note } = LINK_PRESENTATION[key];
      return { key, href, label, ...(icon ? { icon } : {}), ...(note ? { note } : {}), current, attention };
    }),
  };
}

export class PageLinksPresenter extends Presenter<PageLinksViewModel> {
  constructor(current: CurrentTopBar) {
    super(pageLinksViewModel(current.get()));
    current.subscribe((topBar) => this.set(pageLinksViewModel(topBar)));
  }
}
