import type { ChangeEvent } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { Layer, Row } from "../../domain/ledger";
import type { AgentSystemPresenter, ErrorEntry } from "../presenters/agent-system-presenter";

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
          {vm.refused.length > 0 && (
            <div className="[ banner ]" data-tone="danger" role="alert" data-refused="">
              <div className="[ flow ] [ flow-space-3xs grow ]">
                <span>
                  <b>Not saved.</b> {vm.refused.length === 1 ? "1 field was" : `${vm.refused.length} fields were`} refused. Your values are kept.
                </span>
                <span className="[ code ]">
                  {vm.refused.map((e) => (
                    <span key={e.field} style={{ display: "block" }}>{`${e.code} · ${e.field}`}</span>
                  ))}
                </span>
              </div>
            </div>
          )}
          {vm.problem !== "" && (
            <div className="[ banner ]" data-tone="danger" role="alert" data-problem="">
              <div className="[ flow ] [ flow-space-3xs grow ]">
                <span>The save did not go through. Your values are kept.</span>
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
          {vm.status === "ready" && vm.layers.map((layer) => <LayerSection key={layer.key} layer={layer} presenter={presenter} skillsHref={vm.skillsHref} />)}
          {vm.status === "ready" && <SaveBar presenter={presenter} unsaved={vm.unsaved} errors={vm.errors} canSave={vm.canSave} saving={vm.saving} label={vm.saveLabel} />}
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

function LayerSection({ layer, presenter, skillsHref }: { layer: Layer; presenter: AgentSystemPresenter; skillsHref: string }) {
  return (
    <section className="[ as-layer ]" id={`as-${layer.key}`} aria-labelledby={`as-${layer.key}-head`}>
      <header>
        <h2 id={`as-${layer.key}-head`}>{layer.title}</h2>
        <p>{layer.purpose}</p>
      </header>
      <div className="[ ledger ]">
        {layer.rows.map((row) => (
          <LedgerRow key={row.label} row={row} presenter={presenter} />
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

function LedgerRow({ row, presenter }: { row: Row; presenter: AgentSystemPresenter }) {
  return (
    <div className="[ ledger-row ]" data-path={row.path} data-changed={row.draft ? "" : undefined} data-error={row.error ? "" : undefined}>
      <div className="[ lbl ]">
        {row.label}
        <small>{row.hint}</small>
      </div>
      <div className="[ ctl ]">
        {row.kind === "toggle" ? (
          <button type="button" className="[ switch ]" role="switch" aria-checked={row.value} aria-label={row.label} aria-invalid={row.error ? true : undefined} onClick={() => presenter.edit(row.path, !row.value)}>
            <span className="[ track ]" />
            <span>{row.value ? "On" : "Off"}</span>
          </button>
        ) : (
          <span className="[ color-ink-muted ]">{row.text}</span>
        )}
        {row.error && (
          <span className="[ err ]" role="alert" data-error-for={row.path}>
            <code>{row.error.code}</code> · {row.error.message}
          </span>
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
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={() => presenter.overrideHere(row.path)}>
            Override
          </button>
        )}
        {row.action === "reset" && (
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={() => presenter.reset(row.path)}>
            Reset
          </button>
        )}
      </div>
    </div>
  );
}

/** The sticky bar: how many rows are unsaved, what stands in the way (by section and field), Discard and Save. */
function SaveBar({ presenter, unsaved, errors, canSave, saving, label }: { presenter: AgentSystemPresenter; unsaved: number; errors: readonly ErrorEntry[]; canSave: boolean; saving: boolean; label: string }) {
  if (unsaved === 0 && errors.length === 0) return null;
  return (
    <div className="[ save-bar ]" role="region" aria-label="Unsaved changes">
      <span className="[ count ]" data-unsaved={unsaved}>
        {unsaved === 1 ? "1 unsaved change" : `${unsaved} unsaved changes`}
      </span>
      {errors.length > 0 && (
        <span className="[ errs ]" role="alert">
          {errors.length === 1 ? "1 error" : `${errors.length} errors`} · {errors.map((e) => `${e.section} › ${e.label}`).join(", ")}
        </span>
      )}
      <span className="[ end ]">
        <button type="button" className="[ button ]" data-variant="ghost" disabled={saving} onClick={() => presenter.discard()}>
          Discard
        </button>
        <button type="button" className="[ button ]" data-variant={canSave ? "primary" : undefined} aria-disabled={!canSave} aria-busy={saving ? true : undefined} onClick={() => void presenter.save()}>
          {label}
        </button>
      </span>
    </div>
  );
}
