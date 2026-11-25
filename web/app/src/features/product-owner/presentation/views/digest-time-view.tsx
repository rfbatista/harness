import { useEffect, useRef } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { DigestTimePresenter } from "../presenters/digest-time-presenter";

/** The "Daily digest" block of the Settings section: a native time input beside the zone and the next run. */
export function DigestTimeView({ presenter }: { presenter: DigestTimePresenter }) {
  const vm = usePresenter(presenter);
  const input = useRef<HTMLInputElement>(null);
  const clear = useRef<HTMLButtonElement>(null);
  // The input is the person's while they type; it goes back to what the server holds when a change is refused or another tab changes it.
  useEffect(() => {
    if (input.current && input.current.value !== vm.value) input.current.value = vm.value;
  }, [vm.value, vm.revision]);
  // The time the person ended on: on leaving the input or on Enter, never while they type. A half-typed time goes back to the saved one.
  const commit = (event?: { relatedTarget?: EventTarget | null }) => {
    // Going to Clear time: the person asks for no time, so the typed one is not saved on the way.
    if (event?.relatedTarget && event.relatedTarget === clear.current) return;
    const typed = input.current?.value ?? "";
    void presenter.change(typed);
    if (input.current && !/^\d\d:\d\d$/.test(typed)) input.current.value = vm.value;
  };
  return (
    <section className="[ digest-time ]" aria-labelledby="digest-time-heading" data-dimmed={vm.dimmed ? "" : undefined}>
      <h2 id="digest-time-heading" style={{ fontSize: "var(--text-base)" }}>
        Daily digest
      </h2>
      {vm.state === "error" && (
        <p className="[ error ]" role="alert">
          {vm.error}{" "}
          <button type="button" className="[ button ]" data-size="sm" onClick={presenter.retry}>
            Try again
          </button>
        </p>
      )}
      {vm.state !== "error" && (
        <div className="[ field ]">
          <label htmlFor="digest-time">Digest time</label>
          <div className="[ timegrid ]">
            <input ref={input} id="digest-time" type="time" defaultValue={vm.value} disabled={vm.state === "loading"} aria-busy={vm.busy} aria-describedby="digest-time-note digest-time-hint" onBlur={commit}
              onKeyDown={(event) => {
                if (event.key === "Enter") commit();
                // Escape gives the saved time back.
                else if (event.key === "Escape" && input.current) input.current.value = vm.value;
              }}
            />
            <span id="digest-time-note" className="[ inline-note ]">
              {vm.note}
            </span>
          </div>
          <div className="[ cluster ]">
            {vm.canClear && (
              <button ref={clear} type="button" className="[ button ]" data-size="sm" onClick={() => void presenter.clear()}>
                Clear time
              </button>
            )}
          </div>
          {vm.saveError !== "" && (
            <p className="[ error ]" role="alert">
              {vm.saveError}
            </p>
          )}
          <span id="digest-time-hint" className="[ hint ]">
            {vm.hint} <a href={vm.browseHref}>Browse digests</a>
          </span>
        </div>
      )}
    </section>
  );
}
