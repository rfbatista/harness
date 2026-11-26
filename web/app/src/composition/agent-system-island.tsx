// The Agent system page: a client-rendered island in <main>, the ledger of the four layers' settings.

import { createRoot } from "react-dom/client";
import type { ApiClient } from "../shared/infrastructure/api-client";
import { httpSettingsSource } from "../features/agent-system/infrastructure/http-settings-source";
import { browserScopeAddress } from "../features/agent-system/infrastructure/browser-scope-address";
import { SettingsPropsDto } from "../features/agent-system/infrastructure/dto/settings-props-dto";
import { AgentSystemPresenter } from "../features/agent-system/presentation/presenters/agent-system-presenter";
import { AgentSystemView } from "../features/agent-system/presentation/views/agent-system-view";

export function mountAgentSystem(mount: Element, props: unknown, win: Window, api: ApiClient): () => void {
  const { project_id, project_name } = SettingsPropsDto.fromJSON(props);
  const presenter = new AgentSystemPresenter({
    source: httpSettingsSource(api),
    address: browserScopeAddress(win),
    projectId: project_id,
    projectName: project_name,
    architectureHref: (repository) => `/projects/${encodeURIComponent(project_id)}/repositories/${encodeURIComponent(repository)}/architecture/instructions`,
  });
  const root = createRoot(mount);
  root.render(<AgentSystemView presenter={presenter} />);
  void presenter.start();
  return () => {
    presenter.stop();
    root.unmount();
  };
}
