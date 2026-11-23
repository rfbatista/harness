import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { ProjectPickerPresenter } from "../presenters/project-picker-presenter";

/** The picker is a GET form, so it works without JavaScript; choosing submits it through the presenter. */
export function ProjectPickerView({ presenter }: { presenter: ProjectPickerPresenter }) {
  const vm = usePresenter(presenter);
  if (!vm.visible) return null;
  return (
    <form className="[ cluster ] [ gutter-xs ]" method="get" action={vm.action}>
      <label className="[ visually-hidden ]" htmlFor="project-picker">
        Project
      </label>
      <select id="project-picker" name="project" data-size="sm" data-fit="" value={vm.selected} onChange={(event) => presenter.choose(event.currentTarget.value)}>
        {vm.showPlaceholder && (
          <option value="" disabled>
            {vm.placeholder}
          </option>
        )}
        {vm.options.map((option) => (
          <option key={option.id} value={option.id}>
            {option.name}
          </option>
        ))}
      </select>
      <noscript>
        <button type="submit" className="[ button ]" data-size="sm">
          Open
        </button>
      </noscript>
    </form>
  );
}
