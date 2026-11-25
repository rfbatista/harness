// The top bar: an app-shell composition of the two features' views. It renders
// what follows the Tasks button in <header class="topbar">.

import { PageLinksView } from "../features/navigation/presentation/views/page-links-view";
import { ProjectPickerView } from "../features/navigation/presentation/views/project-picker-view";
import type { PageLinksPresenter } from "../features/navigation/presentation/presenters/page-links-presenter";
import type { ProjectPickerPresenter } from "../features/navigation/presentation/presenters/project-picker-presenter";
import { BellView } from "../features/notifications/presentation/views/bell-view";
import type { BellPresenter } from "../features/notifications/presentation/presenters/bell-presenter";
import type { PanelPresenter } from "../features/notifications/presentation/presenters/panel-presenter";
import type { TogglePresenter } from "../features/product-owner/presentation/presenters/toggle-presenter";
import { ToggleView } from "../features/product-owner/presentation/views/toggle-view";

export interface TopBarPresenters {
  readonly picker: ProjectPickerPresenter;
  /** The product owner's toggle, right after the project picker. */
  readonly owner: TogglePresenter;
  readonly links: PageLinksPresenter;
  /** Absent when the server does not serve the inbox. */
  readonly bell: { readonly presenter: BellPresenter; readonly panel: PanelPresenter } | null;
}

export function TopBarView({ presenters }: { presenters: TopBarPresenters }) {
  return (
    <>
      <a className="[ brand ]" href="/">
        harness
      </a>
      <ProjectPickerView presenter={presenters.picker} />
      <ToggleView presenter={presenters.owner} />
      <PageLinksView presenter={presenters.links} />
      {presenters.bell && <BellView presenter={presenters.bell.presenter} panel={presenters.bell.panel} />}
    </>
  );
}
