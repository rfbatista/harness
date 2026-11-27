// The Score page: a client-rendered island in <main>, the eval bench's scorecard beside a baseline, its cases and its runs.

import { createRoot } from "react-dom/client";
import type { ApiClient } from "../shared/infrastructure/api-client";
import type { Feed } from "../shared/kernel/feed";
import { systemClock } from "../shared/kernel/clock";
import { httpScoreFeed } from "../features/score/infrastructure/http-score-feed";
import { httpScoreSource } from "../features/score/infrastructure/http-score-source";
import { jsonCallsText } from "../features/score/infrastructure/dto/json-calls-text";
import { ScorePropsDto } from "../features/score/infrastructure/dto/score-props-dto";
import { ScorePresenter } from "../features/score/presentation/presenters/score-presenter";
import { ScoreView } from "../features/score/presentation/views/score-view";

const TICK_MS = 1_000;

export function mountScore(mount: Element, props: unknown, win: Window, api: ApiClient, feed: Feed): () => void {
  const { project_id, project_name } = ScorePropsDto.fromJSON(props);
  const presenter = new ScorePresenter({
    source: httpScoreSource(api),
    feed: httpScoreFeed(feed),
    calls: jsonCallsText,
    projectId: project_id,
    projectName: project_name,
    now: () => new Date(systemClock.now()),
  });
  const root = createRoot(mount);
  root.render(<ScoreView presenter={presenter} />);
  void presenter.start();
  // The elapsed time of a running run counts on.
  const tick = win.setInterval(() => presenter.tick(), TICK_MS);
  return () => {
    win.clearInterval(tick);
    presenter.stop();
    root.unmount();
  };
}
