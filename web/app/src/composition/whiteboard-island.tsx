// The agents whiteboard island of the browser: built from its props alone and
// rendered on the client (the task page's list is Alpine's, so there is no
// server render to hydrate).

import { createRoot } from "react-dom/client";
import type { ApiClient } from "../shared/infrastructure/api-client";
import { httpBoardSource } from "../features/whiteboard/infrastructure/http-board-source";
import { browserStorage, storageBoardPreferences } from "../features/whiteboard/infrastructure/storage-board-preferences";
import { BoardPresenter } from "../features/whiteboard/presentation/presenters/board-presenter";
import { BoardView } from "../features/whiteboard/presentation/views/board-view";
import { WhiteboardPropsDto } from "./whiteboard-props-dto";

export function mountWhiteboard(mount: Element, rawProps: unknown, api: ApiClient): () => void {
  const props = WhiteboardPropsDto.fromJSON(rawProps);
  const presenter = new BoardPresenter({
    source: httpBoardSource(api),
    preferences: storageBoardPreferences(browserStorage()),
    ticketId: props.ticket_id,
    projectId: props.project_id,
    agentNames: props.agent_names,
    taskTitle: props.task_title,
    now: () => new Date(),
  });
  const root = createRoot(mount);
  root.render(<BoardView presenter={presenter} />);
  void presenter.start();
  return () => root.unmount();
}
