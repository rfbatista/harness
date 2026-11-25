import { useEffect, useRef } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { EntryRow, MemoryTabPresenter } from "../presenters/memory-tab-presenter";
import { OwnerIcon } from "./owner-icon";

const idOf = (entryId: string): string => `memory-entry-${entryId}`;

/** The Memory tab: the entries, each edited where it stands or deleted after a danger confirm. */
export function MemoryTabView({ presenter, onOpenThread }: { presenter: MemoryTabPresenter; onOpenThread: (threadId: string) => void }) {
  const vm = usePresenter(presenter);
  const list = useRef<HTMLDivElement>(null);
  const seen = useRef(0);

  // Focus goes where the presenter asks, once per request.
  useEffect(() => {
    const { entryId, target, seq } = vm.focus;
    if (seq === 0 || seq === seen.current) return;
    const row = list.current?.querySelector<HTMLElement>(`#${CSS.escape(idOf(entryId))}`);
    const wanted: Record<string, string> = { text: "textarea", "edit-button": "[data-act=edit]", "delete-button": "[data-act=delete]", "delete-confirm": "[data-act=keep]", receipt: "", entry: "" };
    const node = target === "" || row == null ? null : wanted[target] === "" ? row : row.querySelector<HTMLElement>(wanted[target]!);
    // A row that is not drawn yet (the list is still reading) asks again on the next change.
    if (node == null) return;
    seen.current = seq;
    node.focus();
    if (target === "entry") node.scrollIntoView({ block: "nearest" });
  }, [vm.focus, vm.rows]);

  return (
    <div className="[ memory-panel ]">
      <div className="[ memory-head ]">
        <p>{vm.loading ? "Reading what the product owner remembers…" : vm.sentence}</p>
        <div className="[ search ]">
          <OwnerIcon name="search" />
          <label className="[ visually-hidden ]" htmlFor="po-memory-filter">
            Filter entries
          </label>
          <input id="po-memory-filter" type="search" placeholder="Filter entries" value={vm.filter} onChange={(event) => presenter.setFilter(event.currentTarget.value)} />
        </div>
      </div>
      {vm.error !== "" && (
        <div className="[ banner ]" data-tone="danger" role="alert" style={{ margin: "0 var(--space-l)" }}>
          <span>{vm.error}</span>
          <button type="button" className="[ button ]" data-size="sm" onClick={presenter.retry}>
            Try again
          </button>
        </div>
      )}
      <div ref={list} className="[ memory-list ]" role="list">
        {vm.empty && (
          <div className="[ memory-empty ]">
            <h2>Nothing remembered yet</h2>
            <p>When you tell the product owner what you decided, what you prefer or what you agreed, it saves one entry for each, and they appear here.</p>
          </div>
        )}
        {vm.noMatch && <p className="[ memory-empty ]">No entry matches “{vm.filter}”.</p>}
        {vm.rows.map((row) =>
          row.kind === "receipt" ? (
            <div key={row.key} id={idOf(row.key.slice("receipt-".length))} className="[ memory-entry ]" role="listitem" data-receipt="" tabIndex={-1}>
              <span>{row.text}</span>
            </div>
          ) : (
            <EntryView key={row.key} row={row} presenter={presenter} onOpenThread={onOpenThread} />
          ),
        )}
      </div>
      <div className="[ visually-hidden ]" role="status">
        {vm.loading ? "Reading memory" : ""}
      </div>
    </div>
  );
}

function EntryView({ row, presenter, onOpenThread }: { row: EntryRow; presenter: MemoryTabPresenter; onOpenThread: (threadId: string) => void }) {
  const editing = row.mode === "edit" && row.edit !== null;
  return (
    <div id={idOf(row.entryId)} className="[ memory-entry ]" role="listitem" tabIndex={-1} data-fresh={row.fresh ? "" : undefined} data-editing={editing ? "" : undefined}>
      {editing ? (
        <form
          className="[ edit ]"
          onSubmit={(event) => {
            event.preventDefault();
            void presenter.save();
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.stopPropagation();
              presenter.cancel();
            }
          }}
        >
          <label className="[ visually-hidden ]" htmlFor={`${idOf(row.entryId)}-text`}>
            Entry text
          </label>
          <textarea id={`${idOf(row.entryId)}-text`} value={row.edit!.draft} aria-invalid={row.edit!.over || undefined} aria-describedby={`${idOf(row.entryId)}-hint`} onChange={(event) => presenter.setDraft(event.currentTarget.value)} />
          <div className="[ edit-acts ]">
            <button type="submit" className="[ button ]" data-variant="primary" data-size="sm" disabled={!row.edit!.canSave}>
              Save
            </button>
            <button type="button" className="[ button ]" data-size="sm" onClick={presenter.cancel}>
              Cancel
            </button>
            <span id={`${idOf(row.entryId)}-hint`} className="[ hint ]">
              {row.edit!.showCount ? `${row.edit!.length} / ${row.edit!.limit} · ` : ""}
              Counts from the next message. The product owner is told you edited it.
            </span>
          </div>
          {row.edit!.error !== "" && (
            <p role="alert" className="[ field-error ]">
              {row.edit!.error}
            </p>
          )}
        </form>
      ) : (
        <p className="[ text ]">{row.text}</p>
      )}
      <div className="[ meta ]">
        <span>{row.saved}</span>
        {row.threadHref !== null && (
          <span>
            from{" "}
            <a
              href={row.threadHref}
              onClick={(event) => {
                if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
                event.preventDefault();
                onOpenThread(row.threadId);
              }}
            >
              {row.threadTitle}
            </a>
          </span>
        )}
        {row.edited !== "" && <span>· {row.edited}</span>}
      </div>
      <div className="[ acts ]">
        {!editing && (
          <>
            <button type="button" data-act="edit" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Edit entry" title="Edit entry" onClick={() => presenter.startEdit(row.entryId)}>
              <OwnerIcon name="pencil" />
            </button>
            <button type="button" data-act="delete" className="[ button ]" data-variant="ghost" data-size="sm" data-icon="" aria-label="Delete entry" title="Delete entry" aria-expanded={row.mode === "delete"} onClick={() => presenter.askDelete(row.entryId)}>
              <OwnerIcon name="trash" />
            </button>
          </>
        )}
      </div>
      {row.mode === "delete" && (
        <div className="[ confirm-strip ]" data-tone="danger" role="group" aria-labelledby={`${idOf(row.entryId)}-confirm`}>
          <h3 id={`${idOf(row.entryId)}-confirm`}>Delete this entry?</h3>
          <p>The product owner stops relying on it from the next message. There is no undo; it can only learn it again from you.</p>
          {row.deleteError !== "" && <p role="alert">{row.deleteError}</p>}
          <div className="[ actions ]">
            <button type="button" className="[ button ]" data-variant="danger" onClick={() => void presenter.confirmDelete()}>
              Delete entry
            </button>
            <button type="button" data-act="keep" className="[ button ]" onClick={presenter.keep}>
              Keep it
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
