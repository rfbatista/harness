import type { Ref } from "react";

/** The composer at the panel's foot. In this piece it only holds the place and the focus: sending comes with the thread. */
export function ComposerView({ textarea, placeholder, disabled }: { textarea: Ref<HTMLTextAreaElement>; placeholder: string; disabled: boolean }) {
  return (
    <form className="[ composer ]" data-disabled={disabled ? "" : undefined} onSubmit={(event) => event.preventDefault()}>
      <div className="[ box ]">
        <label className="[ visually-hidden ]" htmlFor="po-composer">
          Message to the product owner
        </label>
        <textarea id="po-composer" ref={textarea} rows={1} placeholder={placeholder} disabled={disabled} />
      </div>
    </form>
  );
}
