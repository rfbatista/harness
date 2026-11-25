import { useRef, useState, type RefObject } from "react";
import { usePopup } from "../../../../shared/presentation/use-popup";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { AboutPresenter } from "../presenters/about-presenter";

/** About this thread: its facts, Rename, Copy link, and the way to its Context tab. */
export function AboutPopupView({ presenter, open, anchor, onClose }: { presenter: AboutPresenter; open: boolean; anchor: RefObject<HTMLElement | null>; onClose: () => void }) {
  const vm = usePresenter(presenter);
  const panel = useRef<HTMLElement>(null);
  const [draft, setDraft] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  usePopup(open, panel, anchor, () => {
    setDraft(null);
    onClose();
  });

  const copy = (event: React.MouseEvent<HTMLElement>) => {
    const win = event.currentTarget.ownerDocument.defaultView;
    if (!win) return;
    void win.navigator.clipboard.writeText(`${win.location.origin}${vm.link}`).then(() => setCopied(true));
  };
  const save = async () => {
    if (draft !== null && (await presenter.rename(draft))) setDraft(null);
  };

  return (
    <section ref={panel} className="[ popup ] [ about ]" aria-label="About this thread" onKeyDown={(event) => void (event.key === "Escape" && (event.stopPropagation(), setDraft(null), onClose()))}>
      <h3>About this thread</h3>
      {!vm.available ? (
        <p>A thread starts with your first message.</p>
      ) : (
        <>
          {draft === null ? (
            <p>
              <strong>{vm.title}</strong>
            </p>
          ) : (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void save();
              }}
            >
              <label className="[ visually-hidden ]" htmlFor="po-rename">
                Thread title
              </label>
              <input id="po-rename" value={draft} maxLength={200} onChange={(event) => setDraft(event.currentTarget.value)} autoFocus />
              {vm.error !== "" && <p role="alert">{vm.error}</p>}
              <div className="[ cluster ] [ gutter-xs ]">
                <button type="submit" className="[ button ]" data-variant="primary" data-size="sm">
                  Save
                </button>
                <button type="button" className="[ button ]" data-size="sm" onClick={() => setDraft(null)}>
                  Cancel
                </button>
              </div>
            </form>
          )}
          <dl>
            <dt>Started</dt>
            <dd>{vm.started === "" ? "—" : vm.started}</dd>
            <dt>Messages</dt>
            <dd>{vm.messages}</dd>
          </dl>
          <div className="[ cluster ] [ gutter-xs ]">
            <button type="button" className="[ button ]" data-size="sm" onClick={() => setDraft(vm.title)}>
              Rename
            </button>
            <button type="button" className="[ button ]" data-size="sm" onClick={copy}>
              {copied ? "Link copied" : "Copy link"}
            </button>
          </div>
          {vm.contextHref !== null && (
            <p className="[ prompt-peek ]">
              The agent, prompt and model it started with: <a href={vm.contextHref}>Context tab</a>
            </p>
          )}
        </>
      )}
    </section>
  );
}
