import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { DigestItemViewModel, DigestsTabPresenter, DigestViewModel } from "../presenters/digests-tab-presenter";
import { MarkdownView } from "./markdown-view";
import { OwnerIcon, OwnerMark } from "./owner-icon";

const noTask = (): "idle" => "idle";

/** The Digests tab: the dates beside the selected digest. Its parts are the grid items of <main class="owner-page">. */
export function DigestsTabView({ presenter }: { presenter: DigestsTabPresenter }) {
  const vm = usePresenter(presenter);
  if (vm.state === "loading") return <p className="[ digest-empty ]" role="status">Loading digests…</p>;
  if (vm.state === "error") {
    return (
      <div className="[ digest-empty ]">
        <p className="[ error ]" role="alert">
          {vm.error}
        </p>
        <div className="[ cluster ]">
          <button type="button" className="[ button ]" onClick={presenter.retry}>
            Try again
          </button>
        </div>
      </div>
    );
  }
  const off = vm.offNote !== "" && (
    <div className="[ banner ]" data-tone="attention" role="status">
      <OwnerIcon name="info" />
      <span>{vm.offNote}</span>
      <button type="button" className="[ button ]" data-size="sm" disabled={vm.turningOn} onClick={() => void presenter.turnOn()}>
        Turn it on
      </button>
      {vm.turnOnError !== "" && (
        <span className="[ error ]" role="alert">
          {vm.turnOnError}
        </span>
      )}
    </div>
  );
  if (vm.state === "empty") {
    return (
      <div className="[ digest-empty ]">
        {off}
        <h2>No digests yet</h2>
        <p>{vm.emptyText}</p>
        <div className="[ cluster ]">
          <a className="[ button ]" href={vm.timeHref}>
            {vm.timeLabel === "Set time" ? "Set the time" : "Change the time"}
          </a>
        </div>
      </div>
    );
  }
  return (
    <div className="[ digests ]">
      <nav className="[ dates ]" aria-label="Digests by date">
        <div className="[ dates-head ]">
          <span>{vm.head}</span>
          <a href={vm.timeHref}>{vm.timeLabel}</a>
        </div>
        {vm.rows.map((row) => {
          if (row.kind === "month") {
            return (
              <div key={`m${row.label}`} className="[ month ]">
                {row.label}
              </div>
            );
          }
          if (row.kind === "gap") {
            return (
              <div key={`g${row.label}`} className="[ date-row ]" data-gap="">
                <span className="[ d ]">{row.label}</span>
                <span className="[ c ]">nothing recorded</span>
              </div>
            );
          }
          return (
            <a
              key={row.date}
              className="[ date-row ]"
              href={`#digests/${row.date}`}
              aria-current={row.current ? "page" : undefined}
              onClick={(event) => {
                event.preventDefault();
                presenter.select(row.date);
              }}
            >
              <span className="[ d ]">{row.label}</span>
              <time className="[ text-xs ]">{row.time}</time>
              <span className="[ c ]">
                {row.counts}
                {row.counts !== "" && row.attention !== "" && " · "}
                {row.attention !== "" && <b>{row.attention}</b>}
              </span>
            </a>
          );
        })}
        {vm.hasEarlier && (
          <div className="[ earlier ]">
            <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" aria-busy={vm.loadingEarlier} disabled={vm.loadingEarlier} onClick={() => void presenter.loadEarlier()}>
              {vm.loadingEarlier ? "Loading…" : vm.earlierError === "" ? "Load earlier digests" : "Try again"}
            </button>
            {vm.earlierError !== "" && (
              <p className="[ error ]" role="alert">
                {vm.earlierError}
              </p>
            )}
          </div>
        )}
        <p className="[ quiet ]">{vm.gapNote}</p>
      </nav>
      <div className="[ digest-column ]">
        {off}
        {vm.selected && <DigestView digest={vm.selected} />}
      </div>
    </div>
  );
}

function DigestView({ digest }: { digest: DigestViewModel }) {
  return (
    <article className="[ digest ]" aria-labelledby="digest-heading">
      <header>
        <h2 id="digest-heading">{digest.heading}</h2>
        <p>
          <span className="[ by ]">
            <OwnerMark size="sm" />
            <span className="[ n ]">Product owner</span>
          </span>{" "}
          · {digest.covers}.
        </p>
      </header>
      <div className="[ summary ]">
        <MarkdownView blocks={digest.blocks} taskState={noTask} />
      </div>
      {digest.sections.map((section) => (
        <section key={section.key} aria-labelledby={`digest-${section.key}`}>
          <h3 id={`digest-${section.key}`}>
            {section.title} <span className="[ n ]">{section.items.length}</span>
          </h3>
          <ul role="list">
            {section.items.map((item, at) => (
              <li key={at} data-attention={section.key === "waiting" ? "" : undefined}>
                <ItemTitle item={item} />
                <ItemMove item={item} />
              </li>
            ))}
          </ul>
        </section>
      ))}
      <footer>The product owner reads this digest when you ask about these days, for example “what happened on Friday?”.</footer>
    </article>
  );
}

function ItemTitle({ item }: { item: DigestItemViewModel }) {
  return item.href === "" ? <span>{item.title}</span> : <a href={item.href}>{item.title}</a>;
}

function ItemMove({ item }: { item: DigestItemViewModel }) {
  if (item.outcome !== "") {
    return (
      <span className="[ move ]">
        <span className="[ status ]" data-state={item.outcome === "failed" ? "failed" : "done"}>
          {item.outcome}
        </span>
      </span>
    );
  }
  if (item.to === "") return null;
  return (
    <span className="[ move ]">
      {item.from !== "" && (
        <>
          <del>{item.from}</del> →{" "}
        </>
      )}
      <b>{item.to}</b>
    </span>
  );
}
