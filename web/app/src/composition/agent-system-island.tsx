// The Agent system page: a client-rendered island in <main>, the ledger of the four layers' settings.

import { createRoot } from "react-dom/client";
import type { ApiClient } from "../shared/infrastructure/api-client";
import type { Feed } from "../shared/kernel/feed";
import { httpSettingsFeed } from "../features/agent-system/infrastructure/http-settings-feed";
import { httpSettingsSource } from "../features/agent-system/infrastructure/http-settings-source";
import { browserScopeAddress } from "../features/agent-system/infrastructure/browser-scope-address";
import { SettingsPropsDto } from "../features/agent-system/infrastructure/dto/settings-props-dto";
import { AgentSystemPresenter } from "../features/agent-system/presentation/presenters/agent-system-presenter";
import { AgentSystemView } from "../features/agent-system/presentation/views/agent-system-view";

export function mountAgentSystem(mount: Element, props: unknown, win: Window, api: ApiClient, feed: Feed): () => void {
  const { project_id, project_name, models } = SettingsPropsDto.fromJSON(props);
  const presenter = new AgentSystemPresenter({
    source: httpSettingsSource(api),
    feed: httpSettingsFeed(feed),
    address: browserScopeAddress(win),
    projectId: project_id,
    projectName: project_name,
    models,
    scoreHref: `/projects/${encodeURIComponent(project_id)}/score`,
    architectureHref: (repository) => `/projects/${encodeURIComponent(project_id)}/repositories/${encodeURIComponent(repository)}/architecture/instructions`,
  });
  const root = createRoot(mount);
  root.render(<AgentSystemView presenter={presenter} />);
  void presenter.start();
  // The saved line's age moves on.
  const tick = win.setInterval(() => presenter.tick(), 30_000);
  return () => {
    win.clearInterval(tick);
    presenter.stop();
    root.unmount();
  };
}
