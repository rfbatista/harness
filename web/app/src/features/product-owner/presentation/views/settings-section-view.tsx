import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { SettingsSectionPresenter } from "../presenters/settings-section-presenter";
import { OwnerIcon } from "./owner-icon";

/** What follows the "Product owner" heading of the project's Settings page. */
export function SettingsSectionView({ presenter }: { presenter: SettingsSectionPresenter }) {
  const vm = usePresenter(presenter);
  return (
    <div className="[ flow ] [ flow-space-s ]">
      <p className="[ lead ]">
        One persistent product owner for this project: ask it where the project stands, shape features with it. It reads the whole project read-only and never writes code or starts sessions. <a href={vm.pageHref}>Open the product owner</a>
      </p>
      <button type="button" className="[ switch ]" role="switch" aria-checked={vm.checked} aria-busy={vm.busy} disabled={vm.disabled} onClick={() => void presenter.toggle()}>
        <span className="[ track ]" />
        <span>{vm.label}</span>
      </button>
      <span className="[ inline-note ]">
        <OwnerIcon name="info" />
        {vm.note}
      </span>
      {vm.error !== "" && (
        <p className="[ error ]" role="alert">
          {vm.error}
        </p>
      )}
    </div>
  );
}
