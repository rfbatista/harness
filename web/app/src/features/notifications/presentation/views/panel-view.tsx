import { useEffect, useRef, type MouseEvent, type RefObject } from "react";
import { usePopup } from "../../../../shared/presentation/use-popup";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { PanelPresenter } from "../presenters/panel-presenter";
import type { Row } from "../presenters/row";

const TICK_MS = 30_000;

/** The panel behind the bell. `anchor` is the bell's trigger, which it opens beside. */
export function PanelView({ presenter, anchor }: { presenter: PanelPresenter; anchor: RefObject<HTMLElement | null> }) {
  const vm = usePresenter(presenter);
  const panel = useRef<HTMLElement>(null);
  usePopup(vm.open, panel, anchor, () => presenter.close());

  // The ages move on while the panel is open.
  useEffect(() => {
    if (!vm.open) return;
    const timer = setInterval(presenter.tick, TICK_MS);
    return () => clearInterval(timer);
  }, [vm.open, presenter]);

  // The presenter states where the focus goes; the view carries it out (after the popup is open).
  const focus = vm.focus;
  useEffect(() => {
    if (focus?.target !== "panel") return;
    const element = panel.current;
    (element?.querySelector<HTMLElement>("[data-item]") ?? element?.querySelector<HTMLElement>("h2"))?.focus();
  }, [focus]);

  const onKeyDown = (event: React.KeyboardEvent<HTMLElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      presenter.escape();
      return;
    }
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    const links = [...(panel.current?.querySelectorAll<HTMLElement>("[data-item]") ?? [])];
    if (links.length === 0) return;
    event.preventDefault();
    const at = links.indexOf(document.activeElement as HTMLElement);
    const next = event.key === "ArrowDown" ? (at + 1) % links.length : (at <= 0 ? links.length : at) - 1;
    links[next]?.focus();
  };

  return (
    <section id="notifications-panel" ref={panel} className="[ popup ] [ notifications-panel ]" aria-labelledby="notifications-heading" onKeyDown={onKeyDown}>
      <div className="[ head ]">
        <h2 id="notifications-heading" tabIndex={-1}>
          Notifications
        </h2>
        <div className="[ cluster ] [ gutter-2xs ]" role="group" aria-label="Show">
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" aria-pressed={!vm.unreadOnly} onClick={presenter.showAll}>
            All
          </button>
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" aria-pressed={vm.unreadOnly} onClick={presenter.showUnread}>
            Unread
          </button>
        </div>
      </div>
      {vm.error !== null && (
        <div className="[ message ]" data-tone="danger" role="alert">
          <span>{vm.error}</span>
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={presenter.dismissError}>
            Dismiss
          </button>
        </div>
      )}
      <div className="[ list ]" aria-busy={vm.loading}>
        <ul role="list" className="[ notification-list ]">
          {vm.rows.map((row) => (
            <li key={row.id}>
              <NotificationRow row={row} presenter={presenter} />
            </li>
          ))}
        </ul>
        <p className="[ message ]" style={vm.empty ? undefined : { display: "none" }}>
          {vm.empty ? vm.emptyText : ""}
        </p>
      </div>
      <footer className="[ foot ]">
        <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" disabled={!vm.canMarkAll} onClick={() => void presenter.markAll()}>
          Mark all read
        </button>
        <a className="[ all ]" href={vm.inboxHref}>
          See all notifications
        </a>
      </footer>
    </section>
  );
}

function NotificationRow({ row, presenter }: { row: Row; presenter: PanelPresenter }) {
  const open = (event: MouseEvent<HTMLAnchorElement>) => {
    // A click that opens a new tab keeps the page and only marks the notification.
    const newTab = event.metaKey || event.ctrlKey || event.shiftKey || event.button === 1;
    if (!newTab) event.preventDefault();
    void presenter.openItem(row.id, newTab);
  };
  return (
    <article className="[ notification ]" data-level={row.level} data-state={row.state}>
      <span className="[ marker ]" aria-hidden="true"></span>
      <div className="[ text ]">
        <a className="[ title ]" data-item="" href={row.href} data-id={row.id} aria-label={row.label} onClick={open}>
          {row.title}
        </a>
        <p className="[ meta ]">
          <span>{row.project}</span>
          {row.task !== "" && <span className="[ task ]">{row.task}</span>}
          {row.levelLabel !== "" && <span className="[ level ]">{row.levelLabel}</span>}
          <time dateTime={row.at}>{row.age}</time>
        </p>
        {row.body !== "" && row.sessions.length === 0 && <p className="[ body ]">{row.body}</p>}
        {row.sessions.length > 0 && (
          <ul role="list" className="[ interrupted-sessions ]">
            {row.sessions.map((session) => (
              <li key={session.key} data-state={session.state}>
                <a href={session.href}>{session.label}</a>
                <span className="[ state ]">{session.state}</span>
                {session.reason !== "" && <span className="[ reason ]">{session.reason}</span>}
              </li>
            ))}
          </ul>
        )}
      </div>
      {row.unread && (
        <button type="button" className="[ button ] [ mark ]" data-variant="ghost" data-size="sm" data-id={row.id} aria-label={row.markLabel} onClick={() => void presenter.markOne(row.id)}>
          Mark read
        </button>
      )}
    </article>
  );
}
