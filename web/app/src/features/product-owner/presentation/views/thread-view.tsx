import { useEffect, useLayoutEffect, useRef } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { RowViewModel, ThreadPresenter } from "../presenters/thread-presenter";
import { ComposerView } from "./composer-view";
import { MarkdownView } from "./markdown-view";
import { OwnerIcon, OwnerMark } from "./owner-icon";
import type { Ref } from "react";

const STARTER_ICONS = ["history", "warn", "tasks", "plus"] as const;

const clock = (at: Date): string => `${String(at.getHours()).padStart(2, "0")}:${String(at.getMinutes()).padStart(2, "0")}`;

/** The thread and its composer: one component, in the panel and on the page. */
export function ThreadView({ presenter, textarea }: { presenter: ThreadPresenter; textarea: Ref<HTMLTextAreaElement> }) {
  const vm = usePresenter(presenter);
  const log = useRef<HTMLDivElement>(null);
  const stuck = useRef(true);

  // Earlier items arrive above what is read: the view stays where it was.
  const heightBefore = useRef<number | null>(null);
  const firstKey = vm.rows[0]?.key;
  useLayoutEffect(() => {
    const box = log.current;
    if (box && heightBefore.current !== null) box.scrollTop += box.scrollHeight - heightBefore.current;
    heightBefore.current = null;
  }, [firstKey]);

  // Follow the answer while the person is at the foot; leave them be once they scrolled up.
  useEffect(() => {
    const box = log.current;
    if (box && stuck.current) box.scrollTop = box.scrollHeight;
  }, [vm.revision]);

  return (
    <>
      <div
        ref={log}
        className="[ thread ]"
        role="log"
        aria-live="off"
        aria-label="Conversation"
        aria-busy={vm.live !== null}
        onScroll={(event) => {
          const box = event.currentTarget;
          stuck.current = box.scrollHeight - box.scrollTop - box.clientHeight < 48;
        }}
      >
        {vm.hasEarlier && (
          <button
            type="button"
            className="[ button ] [ older ]"
            data-size="sm"
            onClick={() => {
              heightBefore.current = log.current?.scrollHeight ?? null;
              void presenter.loadEarlier();
            }}
          >
            Earlier messages
          </button>
        )}
        {vm.empty && (
          <div className="[ thread-empty ]">
            <h2>{vm.empty.heading}</h2>
            <p>{vm.empty.lead}</p>
            <div className="[ starters ]" aria-label="Suggested questions">
              {vm.empty.starters.map((starter, n) => (
                <button key={starter} type="button" className="[ button ]" disabled={vm.composer.disabled} onClick={() => void presenter.startWith(starter)}>
                  <OwnerIcon name={STARTER_ICONS[n] ?? "plus"} />
                  {starter}
                </button>
              ))}
            </div>
          </div>
        )}
        {vm.rows.map((row) => (
          <RowView key={row.key} row={row} presenter={presenter} />
        ))}
        {vm.live && (
          <div className="[ msg ]" data-from="product_owner">
            <span className="[ avatar ]">
              <OwnerMark />
            </span>
            <div className="[ owner-live ]">
              <span className="[ status ]" data-state="running">
                {vm.live.label}
              </span>
              {vm.live.step !== "" && <span className="[ step ]">· {vm.live.step}</span>}
            </div>
          </div>
        )}
      </div>
      <div className="[ visually-hidden ]" role="status">
        {vm.announcement}
      </div>
      {vm.error && (
        <div className="[ banner ]" data-tone="danger" role="alert" style={{ margin: "0 var(--space-m)" }}>
          <span>
            {vm.error.text} <code>{vm.error.code}</code>
            {vm.error.fixUrl && <> · <a href={vm.error.fixUrl}>Fix it</a></>}
          </span>
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={presenter.dismissError}>
            Dismiss
          </button>
        </div>
      )}
      <ComposerView presenter={presenter} vm={vm.composer} textarea={textarea} />
    </>
  );
}

function RowView({ row, presenter }: { row: RowViewModel; presenter: ThreadPresenter }) {
  switch (row.kind) {
    case "message":
      return (
        <article className="[ msg ]" data-from={row.from} data-pending={row.sending ? "" : undefined} data-streaming={row.streaming ? "" : undefined} data-fresh={row.fromOtherTab ? "" : undefined}>
          {row.from === "person" ? (
            <span className="[ avatar ] [ person-mark ]" aria-hidden="true">
              Y
            </span>
          ) : (
            <span className="[ avatar ]">
              <OwnerMark />
            </span>
          )}
          <div className="[ who ]">
            <span className="[ name ]">{row.who}</span>
            <time dateTime={row.at.toISOString()}>{clock(row.at)}</time>
            {row.sending && <span>· sending</span>}
            {row.stopped && <span>· stopped</span>}
            {row.fromOtherTab && <span>· from another tab</span>}
          </div>
          <div className="[ body ]">
            <MarkdownView blocks={row.blocks} taskState={presenter.taskState} />
            {row.streaming && <span className="[ caret ]" aria-hidden="true" />}
          </div>
        </article>
      );
    case "notice":
      return (
        <div className="[ banner ]" data-tone="attention" role="status">
          <span>{row.text}</span>
        </div>
      );
    case "failure":
      return (
        <div className="[ banner ]" data-tone="danger" role="alert">
          <span>
            {row.text} Your message is kept; nothing was changed in the project. <code>{row.code}</code>
          </span>
          {row.canRetry && (
            <button type="button" className="[ button ]" data-size="sm" onClick={() => void presenter.retry(row.retryItemId)}>
              Try again
            </button>
          )}
        </div>
      );
    case "other":
      return (
        <div className="[ msg-note ]">
          <OwnerIcon name="info" />
          <span>
            <span className="[ visually-hidden ]">{row.label}: </span>
            {row.text}
          </span>
        </div>
      );
  }
}
