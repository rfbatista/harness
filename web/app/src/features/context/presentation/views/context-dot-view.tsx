import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { ContextDotPresenter } from "../presenters/context-dot-presenter";

/** The dot on the Context tab's button while the context changed behind another tab. */
export function ContextDotView({ presenter }: { presenter: ContextDotPresenter }) {
  const { dot } = usePresenter(presenter);
  if (!dot) return null;
  return (
    <span className="[ badge ]" data-dot title="The context changed">
      <span className="[ visually-hidden ]">The context changed</span>
    </span>
  );
}
