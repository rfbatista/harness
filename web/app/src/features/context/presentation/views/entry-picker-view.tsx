import type { ContextPresenter } from "../presenters/context-presenter";
import type { EntryPicker } from "../presenters/context-view-model";

/** The small picker of a resumed session: "Started 20:49 · Resumed 09:12". */
export function EntryPickerView({ picker, presenter }: { picker: EntryPicker; presenter: ContextPresenter }) {
  return (
    <label className="[ field ]">
      <span className="[ muted ]">Entry</span>
      <select value={picker.selected} onChange={(event) => presenter.selectEntry(event.target.value)}>
        {picker.options.map((option) => (
          <option key={option.id} value={option.id}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
}
