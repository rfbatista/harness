import type { Ref } from "react";
import type { ThreadPresenter, ThreadViewModel } from "../presenters/thread-presenter";
import { OwnerIcon } from "./owner-icon";

/** The composer at the foot of a thread: a box that grows, Enter sends, Shift+Enter breaks a line, Send turns into Stop while an answer comes. */
export function ComposerView({ presenter, vm, textarea }: { presenter: ThreadPresenter; vm: ThreadViewModel["composer"]; textarea: Ref<HTMLTextAreaElement> }) {
  return (
    <form
      className="[ composer ]"
      data-disabled={vm.disabled ? "" : undefined}
      onSubmit={(event) => {
        event.preventDefault();
        if (vm.mode === "stop") void presenter.stop();
        else if (vm.canSend) void presenter.send();
      }}
    >
      <div className="[ box ]">
        <label className="[ visually-hidden ]" htmlFor="po-composer">
          Message to the product owner
        </label>
        <textarea
          id="po-composer"
          ref={textarea}
          rows={1}
          placeholder={vm.placeholder}
          disabled={vm.disabled}
          value={vm.draft}
          onChange={(event) => presenter.setDraft(event.currentTarget.value)}
          onKeyDown={(event) => {
            if (presenter.handleComposerKey({ key: event.key, shiftKey: event.shiftKey, isComposing: event.nativeEvent.isComposing })) event.preventDefault();
          }}
          // The box grows with what is typed, to twelve lines, then scrolls.
          onInput={(event) => {
            const box = event.currentTarget;
            box.style.blockSize = "auto";
            box.style.blockSize = `${box.scrollHeight}px`;
          }}
        />
        <div className="[ bar ]">
          <span className="[ hint ]">
            <kbd>↵</kbd> send · <kbd>⇧↵</kbd> new line
          </span>
          {vm.mode === "stop" ? (
            <button type="submit" className="[ button ] [ send ]" aria-label="Stop answering" title="Stop">
              <OwnerIcon name="stop" />
            </button>
          ) : (
            <button type="submit" className="[ button ] [ send ]" data-variant="primary" aria-label="Send" title="Send" disabled={!vm.canSend}>
              <OwnerIcon name="send" />
            </button>
          )}
        </div>
      </div>
    </form>
  );
}
