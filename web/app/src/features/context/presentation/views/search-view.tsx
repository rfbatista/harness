import { useEffect, useRef } from "react";
import type { ContextPresenter } from "../presenters/context-presenter";
import type { SearchViewModel } from "../presenters/context-view-model";

/** The search box: "/" asks the presenter for focus, Esc clears. */
export function SearchView({ search, presenter }: { search: SearchViewModel; presenter: ContextPresenter }) {
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const doc = input.current?.ownerDocument;
    if (!doc) return;
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const typing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target?.isContentEditable === true;
      if (event.key === "/" && !typing && input.current?.isConnected) {
        event.preventDefault();
        presenter.focusSearch();
      }
    };
    doc.addEventListener("keydown", onKey);
    return () => doc.removeEventListener("keydown", onKey);
  }, [presenter]);
  useEffect(() => {
    if (search.focusRequest > 0) input.current?.focus();
  }, [search.focusRequest]);
  return (
    <div className="[ toolbar ]">
      <div className="[ search ]">
        <input
          ref={input}
          type="search"
          aria-label="Search the context"
          placeholder="Search names and instruction text"
          value={search.query}
          onChange={(event) => presenter.search(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Escape") presenter.clearSearch();
          }}
        />
      </div>
    </div>
  );
}
