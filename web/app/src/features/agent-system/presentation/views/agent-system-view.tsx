import type { ChangeEvent } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { Layer, Row } from "../../domain/ledger";
import type { AgentSystemPresenter } from "../presenters/agent-system-presenter";

/** The page: toolbar with the scope switch, the sticky index of the layers, and each layer's ledger rows. */
export function AgentSystemView({ presenter }: { presenter: AgentSystemPresenter }) {
  const vm = usePresenter(presenter);
  const onScope = (event: ChangeEvent<HTMLSelectElement>) => void presenter.selectScope(event.target.value);
  const projectScope = vm.scope.kind === "project";
  const chosen = vm.repositories.find((r) => vm.scope.kind === "repository" && r.id === vm.scope.id);
  return (
    <>
      <header className="[ toolbar ]">
        <a className="[ crumb ]" href={`/projects/${encodeURIComponent(vm.projectId)}`}>
          {vm.projectName} /
        </a>
        <h1>Agent system</h1>
        <div className="[ segmented ]" role="group" aria-label="Scope">
          <button type="button" aria-pressed={projectScope} onClick={() => void presenter.selectScope("")}>
            Project defaults
          </button>
          <button type="button" aria-pressed={!projectScope} disabled={vm.repositories.length === 0} onClick={() => vm.repositories[0] && void presenter.selectScope(vm.repositories[0].id)}>
            Repository <span className="[ count ]">{vm.repositories.length}</span>
          </button>
        </div>
        {!projectScope && (
          <span className="[ scope-pick ]">
            <select data-size="sm" data-fit aria-label="Repository" value={vm.scope.kind === "repository" ? vm.scope.id : ""} onChange={onScope}>
              {vm.repositories.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.red ? `${r.name} · missing a required gate` : r.name}
                </option>
              ))}
            </select>
            {chosen?.red && <span className="[ status ]" data-state="missing" data-dot-only role="img" aria-label="missing a required gate" />}
          </span>
        )}
        <span className="[ end ] [ color-ink-faint text-sm ]" role="status">
          {vm.saved}
        </span>
      </header>
      <div className="[ agent-system ]" data-scroll>
        <nav className="[ as-index ]" aria-label="Sections">
          {vm.layers.map((layer) => (
            <a key={layer.key} href={`#as-${layer.key}`}>
              {layer.title}
            </a>
          ))}
        </nav>
        <div className="[ as-main ]">
          {vm.problem !== "" && (
            <div className="[ banner ]" data-tone="danger" role="alert">
              <div className="[ flow ] [ flow-space-3xs grow ]">
                <span>The save was refused. Your values are kept.</span>
                <span className="[ code ]">{vm.problem}</span>
              </div>
            </div>
          )}
          {vm.status === "loading" && <Skeleton />}
          {vm.status === "failed" && (
            <div className="[ banner ]" data-tone="danger" role="alert">
              <div className="[ flow ] [ flow-space-3xs grow ]">
                <span>The agent system settings could not be read.</span>
                <span className="[ code ]">{vm.error}</span>
              </div>
              <button type="button" className="[ button ]" data-size="sm" onClick={() => void presenter.start()}>
                Retry
              </button>
            </div>
          )}
          {vm.status === "ready" && vm.layers.map((layer) => <LayerSection key={layer.key} layer={layer} presenter={presenter} skillsHref={vm.skillsHref} pending={vm.pending} />)}
        </div>
      </div>
    </>
  );
}

function Skeleton() {
  return (
    <div className="[ as-layer ]" aria-busy="true" aria-label="Loading the settings">
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="[ ledger-row ]" data-skeleton="">
          <span className="[ skeleton ]" />
          <span className="[ skeleton ]" />
          <span className="[ skeleton ]" />
          <span />
        </div>
      ))}
    </div>
  );
}

function LayerSection({ layer, presenter, skillsHref, pending }: { layer: Layer; presenter: AgentSystemPresenter; skillsHref: string; pending: readonly string[] }) {
  return (
    <section className="[ as-layer ]" id={`as-${layer.key}`} aria-labelledby={`as-${layer.key}-head`}>
      <header>
        <h2 id={`as-${layer.key}-head`}>{layer.title}</h2>
        <p>{layer.purpose}</p>
      </header>
      <div className="[ ledger ]">
        {layer.rows.map((row) => (
          <LedgerRow key={row.label} row={row} presenter={presenter} busy={pending.includes(row.path)} />
        ))}
        {layer.note !== "" && (
          <div className="[ ledger-row ]" data-note="">
            <div className="[ lbl ]">{layer.key === "learn" ? "Skills" : ""}</div>
            <div className="[ ctl ] [ color-ink-muted ]">
              {layer.note}
              {skillsHref !== "" && layer.key === "learn" && (
                <>
                  {" "}
                  <a href={skillsHref}>Instruction files</a>
                </>
              )}
            </div>
            <div className="[ src ]" />
            <div className="[ act ]" />
          </div>
        )}
      </div>
    </section>
  );
}

function LedgerRow({ row, presenter, busy }: { row: Row; presenter: AgentSystemPresenter; busy: boolean }) {
  return (
    <div className="[ ledger-row ]" data-path={row.path} aria-busy={busy}>
      <div className="[ lbl ]">
        {row.label}
        <small>{row.hint}</small>
      </div>
      <div className="[ ctl ]">
        {row.kind === "toggle" ? (
          <button type="button" className="[ switch ]" role="switch" aria-checked={row.value} aria-label={row.label} disabled={busy} onClick={() => void presenter.change(row.path, !row.value)}>
            <span className="[ track ]" />
            <span>{row.value ? "On" : "Off"}</span>
          </button>
        ) : (
          <span className="[ color-ink-muted ]">{row.text}</span>
        )}
      </div>
      <div className="[ src ]">
        <span className="[ origin ]" data-kind={row.source.kind}>
          {row.source.text}
        </span>
        {row.source.reason !== "" && <span>{row.source.reason}</span>}
        {row.source.replaces !== "" && <span>{row.source.replaces}</span>}
      </div>
      <div className="[ act ]">
        {row.action === "override" && (
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" disabled={busy} onClick={() => void presenter.overrideHere(row.path)}>
            Override
          </button>
        )}
        {row.action === "reset" && (
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" disabled={busy} onClick={() => void presenter.reset(row.path)}>
            Reset
          </button>
        )}
      </div>
    </div>
  );
}
