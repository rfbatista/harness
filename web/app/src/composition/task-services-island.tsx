// The task services panel on the task page: a React island mounted beside the
// whiteboard (ADR 0003). It reads the page's own seed for the task, follows the
// services feed through the page's shared stream, and takes the S key from the
// whiteboard (the board has no S of its own).

import { createRoot } from "react-dom/client";
import type { ApiClient } from "../shared/infrastructure/api-client";
import { readScriptJson } from "../shared/infrastructure/island-props";
import { systemClock } from "../shared/kernel/clock";
import type { Feed } from "../shared/kernel/feed";
import { browserStorage } from "../features/whiteboard/infrastructure/storage-board-preferences";
import { httpServicesSource } from "../features/task-services/infrastructure/http-services-source";
import { storagePanelPreferences } from "../features/task-services/infrastructure/storage-panel-preferences";
import { PanelPresenter, isToggleKey, type PanelMode } from "../features/task-services/presentation/presenters/panel-presenter";
import { PanelView } from "../features/task-services/presentation/views/panel-view";
import { WhiteboardPropsDto } from "./whiteboard-props-dto";

const SEED = "sessions-seed";
const OVERLAY = "(width < 64rem)";
const SHEET = "(width < 48rem)";
const TICK_MS = 30_000;

export function mountTaskServices(mount: Element, win: Window, api: ApiClient, feed: Feed): () => void {
  const props = WhiteboardPropsDto.fromJSON(readScriptJson(win.document, SEED));
  const presenter = new PanelPresenter({
    source: httpServicesSource(api, feed),
    preferences: storagePanelPreferences(browserStorage()),
    clipboard: { copy: (text) => win.navigator.clipboard.writeText(text) },
    scheduler: { now: () => systemClock.now(), after: (ms, fn) => { const id = win.setTimeout(fn, ms); return () => win.clearTimeout(id); } },
    projectId: props.project_id,
    taskId: props.ticket_id,
    taskTitle: props.task_title,
  });
  const overlay = win.matchMedia(OVERLAY);
  const sheet = win.matchMedia(SHEET);
  const mode = (): PanelMode => (sheet.matches ? "sheet" : overlay.matches ? "overlay" : "docked");
  const onMode = () => presenter.setMode(mode());
  onMode();
  overlay.addEventListener("change", onMode);
  sheet.addEventListener("change", onMode);

  // A session pane beside the board: the page's split view leaves data-no-detail off while one is open.
  const split = mount.closest(".split-view");
  const onPane = () => presenter.setSessionPane(split !== null && !split.hasAttribute("data-no-detail"));
  const observer = new MutationObserver(onPane);
  if (split) observer.observe(split, { attributes: true, attributeFilter: ["data-no-detail"] });
  onPane();

  // S, with focus in the whiteboard.
  const row = mount.parentElement;
  const onKey = (event: Event) => {
    const e = event as KeyboardEvent;
    if (!(e.target as Element | null)?.closest?.(".whiteboard") || !isToggleKey(e)) return;
    e.preventDefault();
    presenter.toggle();
  };
  row?.addEventListener("keydown", onKey);
  const tick = win.setInterval(() => presenter.tick(), TICK_MS);
  const outside = (e: Event) => {
    if (presenter.viewModel.mode !== "docked" && presenter.viewModel.open && !mount.contains(e.target as Node)) presenter.collapse();
  };
  win.document.addEventListener("pointerdown", outside);

  const root = createRoot(mount);
  root.render(<PanelView presenter={presenter} />);
  void presenter.start();
  return () => {
    overlay.removeEventListener("change", onMode);
    sheet.removeEventListener("change", onMode);
    observer.disconnect();
    row?.removeEventListener("keydown", onKey);
    win.document.removeEventListener("pointerdown", outside);
    win.clearInterval(tick);
    presenter.stop();
    root.unmount();
  };
}
