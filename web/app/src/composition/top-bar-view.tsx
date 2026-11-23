// The top bar: an app-shell composition of the two features' views. It renders
// what follows the Tasks button in <header class="topbar">.

import { PageLinksView } from "../features/navigation/presentation/views/page-links-view";
import { ProjectPickerView } from "../features/navigation/presentation/views/project-picker-view";
import type { PageLinksPresenter } from "../features/navigation/presentation/presenters/page-links-presenter";
import type { ProjectPickerPresenter } from "../features/navigation/presentation/presenters/project-picker-presenter";
import { BellView } from "../features/notifications/presentation/views/bell-view";
import type { BellPresenter } from "../features/notifications/presentation/presenters/bell-presenter";
import type { PanelPresenter } from "../features/notifications/presentation/presenters/panel-presenter";

export interface TopBarPresenters {
  readonly picker: ProjectPickerPresenter;
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
      <PageLinksView presenter={presenters.links} />
      {presenters.bell && <BellView presenter={presenters.bell.presenter} panel={presenters.bell.panel} />}
    </>
  );
}
