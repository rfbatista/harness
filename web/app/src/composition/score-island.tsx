// The Score page: a client-rendered island in <main>, the eval bench's scorecard beside a baseline, its cases and its runs.

import { createRoot } from "react-dom/client";
import type { ApiClient } from "../shared/infrastructure/api-client";
import type { Feed } from "../shared/kernel/feed";
import { systemClock } from "../shared/kernel/clock";
import { httpScoreFeed } from "../features/score/infrastructure/http-score-feed";
import { httpScoreSource } from "../features/score/infrastructure/http-score-source";
import { ScorePropsDto } from "../features/score/infrastructure/dto/score-props-dto";
import { ScorePresenter } from "../features/score/presentation/presenters/score-presenter";
import { ScoreView } from "../features/score/presentation/views/score-view";

export function mountScore(mount: Element, props: unknown, _win: Window, api: ApiClient, feed: Feed): () => void {
  const { project_id, project_name } = ScorePropsDto.fromJSON(props);
  const presenter = new ScorePresenter({
    source: httpScoreSource(api),
    feed: httpScoreFeed(feed),
    projectId: project_id,
    projectName: project_name,
    now: () => new Date(systemClock.now()),
  });
  const root = createRoot(mount);
  root.render(<ScoreView presenter={presenter} />);
  void presenter.start();
  return () => {
    presenter.stop();
    root.unmount();
  };
}
