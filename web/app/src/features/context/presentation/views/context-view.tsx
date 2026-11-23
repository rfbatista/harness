import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { ContextPresenter } from "../presenters/context-presenter";
import { InstructionsView } from "./instructions-view";

/** The Context tab: a passive view of what its presenter says (the Sandbox tab's shape). */
export function ContextView({ presenter }: { presenter: ContextPresenter }) {
  const vm = usePresenter(presenter);
  return (
    <div className="[ sandbox-tab ]" data-scroll data-context-tab>
        {(vm.status === "idle" || vm.status === "loading") && (
          <div className="[ skeletons ]" aria-busy="true">
            <span />
            <span />
            <span />
          </div>
        )}
        {(vm.status === "unrecorded" || vm.status === "not_found") && <p className="[ note ]">{vm.message}</p>}
        {vm.status === "error" && (
          <p className="[ note ]" role="alert">
            {vm.message}{" "}
            <button type="button" className="[ button ]" data-size="sm" onClick={() => presenter.retry()}>
              Retry
            </button>
          </p>
        )}
        {vm.status === "ready" && (
          <section className="[ part ]" aria-labelledby="context-facts-title">
            <h3 id="context-facts-title">What this session was given</h3>
            <dl className="[ facts ]" data-context-facts>
              {vm.facts.map((fact) => (
                <div key={fact.label}>
                  <dt>{fact.label}</dt>
                  <dd>{fact.value}</dd>
                </div>
              ))}
            </dl>
          </section>
        )}
        {vm.status === "ready" && vm.groups && <InstructionsView group={vm.groups.instructions} presenter={presenter} />}
    </div>
  );
}
