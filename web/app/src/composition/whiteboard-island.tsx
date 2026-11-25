// The agents whiteboard on the task page: two islands. `agents-view` has no
// markup: it announces which view the page shows (the list, or the board) and
// keeps the person's choice. `agents-whiteboard` is the board itself, mounted
// by the page while the board is the view. Both read the page's own seed.

import { createRoot } from "react-dom/client";
import type { ApiClient } from "../shared/infrastructure/api-client";
import { readScriptJson } from "../shared/infrastructure/island-props";
import type { Feed } from "../shared/kernel/feed";
import { browserPageBridge } from "../features/whiteboard/infrastructure/browser-page-bridge";
import { httpBoardSource } from "../features/whiteboard/infrastructure/http-board-source";
import { browserStorage, storageBoardPreferences } from "../features/whiteboard/infrastructure/storage-board-preferences";
import { BoardPresenter } from "../features/whiteboard/presentation/presenters/board-presenter";
import { ViewPresenter } from "../features/whiteboard/presentation/presenters/view-presenter";
import { BoardView } from "../features/whiteboard/presentation/views/board-view";
import { WhiteboardPropsDto } from "./whiteboard-props-dto";

/** The id of the task page's seed script. */
const SEED = "sessions-seed";
/** The page's split view shows one side below this width. */
const NARROW = "(width < 64rem)";

export function mountAgentsView(win: Window): () => void {
  const props = WhiteboardPropsDto.fromJSON(readScriptJson(win.document, SEED));
  const { viewHost, signals } = browserPageBridge(win);
  const presenter = new ViewPresenter({ ticketId: props.ticket_id, preferences: storageBoardPreferences(browserStorage()), host: viewHost, signals, search: win.location.search });
  presenter.start();
  return () => presenter.stop();
}

export function mountWhiteboard(mount: Element, win: Window, api: ApiClient, feed: Feed): () => void {
  const props = WhiteboardPropsDto.fromJSON(readScriptJson(win.document, SEED));
  const { boardHost, signals } = browserPageBridge(win);
  const presenter = new BoardPresenter({
    source: httpBoardSource(api, feed),
    preferences: storageBoardPreferences(browserStorage()),
    host: boardHost,
    signals,
    ticketId: props.ticket_id,
    projectId: props.project_id,
    agentNames: props.agent_names,
    taskTitle: props.task_title,
    now: () => new Date(),
  });
  const narrow = win.matchMedia(NARROW);
  const onNarrow = () => presenter.setNarrow(narrow.matches);
  onNarrow();
  narrow.addEventListener("change", onNarrow);
  const root = createRoot(mount);
  root.render(<BoardView presenter={presenter} />);
  void presenter.start().then(() => presenter.follow());
  return () => {
    narrow.removeEventListener("change", onNarrow);
    presenter.stop();
    root.unmount();
  };
}
