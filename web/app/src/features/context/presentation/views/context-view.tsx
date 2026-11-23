import { usePresenter } from "../../../../shared/presentation/use-presenter";
import { useEffect, useRef } from "react";
import type { ContextPresenter } from "../presenters/context-presenter";
import { ChangesView } from "./changes-view";
import { EntryPickerView } from "./entry-picker-view";
import { LiveLineView } from "./live-line-view";
import { SearchView } from "./search-view";
import { InstructionsView } from "./instructions-view";
import { HooksView, SkillsView } from "./skills-view";
import { ServersView } from "./servers-view";
import { ToolsView } from "./tools-view";

/** The Context tab: a passive view of what its presenter says (the Sandbox tab's shape). */
export function ContextView({ presenter }: { presenter: ContextPresenter }) {
  const vm = usePresenter(presenter);
  // A link to a group (ADR 0085's) brings it into sight once it is on the page.
  const root = useRef<HTMLDivElement>(null);
  const reveal = vm.reveal;
  useEffect(() => {
    if (!reveal || vm.status !== "ready") return;
    root.current?.querySelector(`[data-group="${reveal.group}"]`)?.scrollIntoView({ block: "start" });
  }, [reveal?.request, vm.status]);
  return (
    <div ref={root} className="[ sandbox-tab ]" data-scroll data-context-tab>
        {(vm.status === "idle" || vm.status === "loading") && (
          <div className="[ skeletons ]" aria-busy="true">
            <span data-skeleton />
            <span data-skeleton />
            <span data-skeleton />
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
        {vm.status === "ready" && vm.whiteboard && (
          <p>
            <a href={vm.whiteboard.href}>{vm.whiteboard.label}</a>
          </p>
        )}
        {vm.status === "ready" && vm.picker && <EntryPickerView picker={vm.picker} presenter={presenter} />}
        {vm.status === "ready" && vm.ended && <p className="[ note ]">This session has ended. What it was given is below; nothing here changes any more.</p>}
        {vm.status === "ready" && <LiveLineView live={vm.live} presenter={presenter} />}
        {vm.status === "ready" && <SearchView search={vm.search} presenter={presenter} />}
        {vm.status === "ready" && vm.groups && <InstructionsView group={vm.groups.instructions} presenter={presenter} />}
        {vm.status === "ready" && vm.groups && <ToolsView group={vm.groups.tools} presenter={presenter} />}
        {vm.status === "ready" && vm.groups && <ServersView group={vm.groups.servers} presenter={presenter} />}
        {vm.status === "ready" && vm.groups && <SkillsView group={vm.groups.skills} presenter={presenter} />}
        {vm.status === "ready" && vm.groups && <HooksView group={vm.groups.hooks} presenter={presenter} />}
        {vm.status === "ready" && <p className="[ muted ]">Nothing here changes the session. A setting you change reaches the next session.</p>}
        {vm.status === "ready" && vm.groups && <ChangesView group={vm.groups.changes} presenter={presenter} />}
    </div>
  );
}
