import { useEffect } from "react";
import type { ContextPresenter, LiveLine } from "../presenters/context-presenter";

const SHOWN_MS = 10_000;

/** The one line at the top that names the latest change, announced politely. It lets go on its own. */
export function LiveLineView({ live, presenter }: { live: LiveLine | null; presenter: ContextPresenter }) {
  useEffect(() => {
    if (!live) return;
    const id = setTimeout(() => presenter.dismissLive(), SHOWN_MS);
    return () => clearTimeout(id);
  }, [live, presenter]);
  return (
    <div role="status" aria-live="polite">
      {live && (
        <p className="[ live-line ]">
          <span className="[ name ]">{live.text}</span> <time className="[ muted ]">{live.time}</time>{" "}
          <button type="button" className="[ button ]" data-size="sm" onClick={() => presenter.dismissLive()}>
            Dismiss
          </button>
        </p>
      )}
    </div>
  );
}
