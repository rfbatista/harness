import { useEffect } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { RailPresenter, RailViewModel } from "../presenters/rail-presenter";
import { RailIcon } from "./rail-icons";
import { GroupView } from "./rail-rows-view";
import { useRailHost } from "./use-rail-host";

const SHOWN = (shown: boolean) => (shown ? undefined : { display: "none" });

/** The project's pages: the open one is aria-current. */
function ProjectNavView({ vm }: { vm: RailViewModel }) {
  return (
    <nav className="[ nav ]" aria-label="Project">
      <ul role="list">
        {vm.nav.map((item) => (
          <li key={item.key} data-page={item.key} data-project-id={item.running !== null ? vm.projectId : undefined} data-running-services={item.running ?? undefined}>
            <a href={item.href} aria-current={item.current ? "page" : undefined}>
              <RailIcon name={item.icon} />
              <span className="[ label ]">{item.label}</span>
              {item.key === "services" ? (
                <span className="[ badge ]" data-tone="signal" data-nav-running="" role="status" style={SHOWN(item.badge !== "")}>
                  <span>{item.badge}</span>
                  <span className="[ visually-hidden ]"> running</span>
                </span>
              ) : item.key === "reviews" ? (
                <span className="[ badge ]" data-tone="attention" style={SHOWN(item.badge !== "")}>
                  <span>{item.badge}</span>
                  <span className="[ visually-hidden ]"> waiting on you</span>
                </span>
              ) : item.proposals !== null ? (
                // The design-system page keeps this badge's text and visibility (data-nav-proposals): it is drawn once.
                <span className="[ badge ]" data-tone="attention" data-nav-proposals="" title="Proposals waiting on you" aria-label="Proposals waiting on you" style={SHOWN(item.proposals > 0)}>
                  {item.proposals}
                </span>
              ) : null}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}

/** The rail: the project's pages and its tasks in status groups, with the filter and Needs you. `host` is the rail's own element (null on the server). */
export function RailView({ presenter, host }: { presenter: RailPresenter; host: HTMLElement | null }) {
  const vm = usePresenter(presenter);
  useRailHost(host, presenter, vm);
  // What the server painted is the same for everyone; this browser's opened features apply once the island has taken over.
  useEffect(() => presenter.hydrated(), [presenter]);
  return (
    <>
      <div className="[ drawer-head ]">
        <span className="[ truncate ]">{vm.projectName}</span>
        <button type="button" className="[ button ]" data-variant="ghost" data-icon="" aria-label="Close" onClick={presenter.closeDrawer}>
          <RailIcon name="close" />
        </button>
      </div>
      <a className="[ button ]" data-size="sm" href={vm.newTaskHref}>
        <RailIcon name="plus" />
        New task
      </a>
      <ProjectNavView vm={vm} />
      <nav className="[ nav ]" aria-label="Tasks">
        <TasksView presenter={presenter} vm={vm} />
      </nav>
    </>
  );
}

function TasksView({ presenter, vm }: { presenter: RailPresenter; vm: RailViewModel }) {
  if (vm.isEmpty) {
    return (
      <p className="[ notice ]">
        <span>
          <b>No tasks yet.</b> A task is what agents work on: create one, then start a session on it.
        </span>
      </p>
    );
  }
  return (
    <>
      <div className="[ head ]">
        <h2 className="[ title ]">
          Tasks <span className="[ count ]">{vm.total}</span>
        </h2>
        <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" style={SHOWN(vm.showsNeedsYou)} aria-pressed={vm.needsYouOnly} onClick={presenter.toggleNeedsYou}>
          Needs you <span className="[ badge ]" data-tone="attention">{vm.needsYouCount}</span>
        </button>
      </div>
      <div className="[ filter ]">
        <input
          type="search"
          placeholder="Filter tasks"
          aria-label="Filter tasks"
          aria-describedby="rail-filter-result"
          autoComplete="off"
          value={vm.query}
          onChange={(event) => presenter.setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Escape" && vm.query !== "") {
              event.preventDefault();
              presenter.clearQuery();
            } else if (event.key === "ArrowDown") {
              const first = event.currentTarget.closest("nav")?.querySelector<HTMLElement>("a[data-task-id]");
              if (first) {
                event.preventDefault();
                first.focus();
              }
            } else if (event.key === "Enter" && presenter.openFirstMatch()) event.preventDefault();
          }}
        />
        <kbd aria-hidden="true">/</kbd>
      </div>
      <p id="rail-filter-result" className="[ visually-hidden ]" aria-live="polite">
        {vm.filterResult}
      </p>
      {vm.hasNoMatch ? (
        <div className="[ notice ]">
          <span>{vm.noMatchText}</span>
          <button
            type="button"
            className="[ button ]"
            data-size="sm"
            onClick={(event) => {
              presenter.clearQuery();
              event.currentTarget.closest("nav")?.querySelector<HTMLElement>(".filter input")?.focus();
            }}
          >
            Clear
          </button>
        </div>
      ) : null}
      {vm.nothingNeedsYou ? <p className="[ notice ]">Nothing needs you right now.</p> : null}
      <div className="[ groups ]">
        {vm.groups.map((group) => (
          <GroupViewWithFocus key={group.key} presenter={presenter} group={group} turned={vm.turned} />
        ))}
      </div>
    </>
  );
}

/** A group whose folding takes focus with it: a row that has focus in a group the person folds (with the mouse) moves it to the heading. */
function GroupViewWithFocus({ presenter, group, turned }: { presenter: RailPresenter; group: RailViewModel["groups"][number]; turned: boolean }) {
  return (
    <GroupView
      group={group}
      turned={turned}
      onToggleFeature={presenter.toggleFeature}
      onToggleGroup={(key, heading) => {
        const section = heading.closest("section");
        if (!group.collapsed && section?.querySelector("ul")?.contains(section.ownerDocument.activeElement)) {
          presenter.clearFocusedRow();
          heading.focus();
        }
        presenter.toggleGroup(key);
      }}
    />
  );
}
