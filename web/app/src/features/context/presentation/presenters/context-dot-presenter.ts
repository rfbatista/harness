// The dot on the Context tab: it lights when the session's context changes
// while another tab is in front, and goes out when the Context tab comes up.

import { Presenter } from "../../../../shared/kernel/presenter";
import type { ContextFeed, TabFront } from "../../application/ports";

export interface ContextDotViewModel {
  readonly dot: boolean;
}

export class ContextDotPresenter extends Presenter<ContextDotViewModel> {
  #front = false;
  readonly #stops: (() => void)[] = [];

  constructor(feed: ContextFeed, tab: TabFront, { sessionId, projectId, live }: { sessionId: string; projectId: string; live: boolean }) {
    super({ dot: false });
    this.#stops.push(
      tab.subscribe((front) => {
        this.#front = front;
        if (front) this.set({ dot: false });
      }),
    );
    if (!live) return;
    this.#stops.push(
      feed.follow(projectId, (change) => {
        if (change.sessionId === sessionId && !this.#front) this.set({ dot: true });
      }),
    );
  }

  close(): void {
    for (const stop of this.#stops.splice(0)) stop();
  }
}
