import { useRef, type RefObject } from "react";
import { usePopup } from "../../../../shared/presentation/use-popup";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { ThreadListPresenter } from "../presenters/thread-list-presenter";
import { OwnerIcon } from "./owner-icon";

/** The thread list as a popup from the head's Threads button (the page shows it as a side column). */
export function ThreadsPopupView({ presenter, open, anchor, onClose, allHref }: { presenter: ThreadListPresenter; open: boolean; anchor: RefObject<HTMLElement | null>; onClose: () => void; allHref: string }) {
  const panel = useRef<HTMLElement>(null);
  usePopup(open, panel, anchor, onClose);
  return (
    <section ref={panel} className="[ popup ]" aria-label="Threads" style={{ inlineSize: "22rem" }} onKeyDown={(event) => void (event.key === "Escape" && (event.stopPropagation(), onClose()))}>
      <ThreadListView
        presenter={presenter}
        inPopup
        onPicked={onClose}
        footer={
          <a className="[ button ]" data-variant="ghost" data-size="sm" href={allHref}>
            All threads
          </a>
        }
      />
    </section>
  );
}

/** The list itself: New thread, a filter, and threads grouped by day. */
export function ThreadListView({ presenter, inPopup = false, onPicked, onOpen, onNew, footer }: { presenter: ThreadListPresenter; inPopup?: boolean; onPicked?: () => void; onOpen?: (threadId: string) => void; onNew?: () => void; footer?: React.ReactNode }) {
  const vm = usePresenter(presenter);
  return (
    <div className="[ threads ]">
      <div className="[ new ]">
        <button
          type="button"
          className="[ button ]"
          data-size="sm"
          onClick={() => {
            (onNew ?? presenter.newThread)();
            onPicked?.();
          }}
        >
          <OwnerIcon name="plus" />
          New thread
        </button>
      </div>
      <div className="[ search ]">
        <OwnerIcon name="search" />
        <label className="[ visually-hidden ]" htmlFor={`po-thread-filter-${inPopup ? "popup" : "page"}`}>
          Filter threads
        </label>
        <input id={`po-thread-filter-${inPopup ? "popup" : "page"}`} type="search" placeholder="Filter threads" value={vm.query} onChange={(event) => presenter.setQuery(event.currentTarget.value)} />
      </div>
      <div className="[ list ]" data-in-popup={inPopup ? "" : undefined}>
        {vm.error !== "" && (
          <p role="alert" style={{ padding: "var(--space-s) var(--space-m)", color: "var(--color-danger)" }}>
            {vm.error}
          </p>
        )}
        {vm.empty !== null && <p style={{ padding: "var(--space-s) var(--space-m)", color: "var(--color-ink-muted)", fontSize: "var(--text-sm)" }}>{vm.empty}</p>}
        {vm.groups.map((group) => (
          <section key={group.label} aria-label={group.label}>
            <h3 className="[ group ]">{group.label}</h3>
            {group.rows.map((row) => (
              <a
                key={row.threadId}
                className="[ thread-row ]"
                href={row.href}
                data-live={row.live ? "" : undefined}
                aria-current={row.current ? "page" : undefined}
                onClick={(event) => {
                  // A plain click opens the thread here; a modified one keeps its link's own way.
                  if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
                  event.preventDefault();
                  (onOpen ?? presenter.open)(row.threadId);
                  onPicked?.();
                }}
              >
                <span className="[ t ]">
                  {row.live && <span className="[ visually-hidden ]">answering: </span>}
                  {row.title}
                </span>
                <time>{row.time}</time>
                {row.excerpt !== "" && <span className="[ snip ]">{row.excerpt}</span>}
              </a>
            ))}
          </section>
        ))}
      </div>
      {footer && <div style={{ padding: "var(--space-xs) var(--space-s)", borderBlockStart: "var(--border-width) solid var(--color-line)" }}>{footer}</div>}
    </div>
  );
}
