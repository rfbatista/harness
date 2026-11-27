import { useState, type ChangeEvent, type KeyboardEvent } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { GateRow, Layer, Row } from "../../domain/ledger";
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
              {layer.attention > 0 && <span className="[ status ]" data-state="missing" data-dot-only role="img" aria-label={vm.scope.kind === "project" ? `${layer.attention} red ${layer.attention === 1 ? "repository" : "repositories"}` : `${layer.attention} missing`} style={{ marginInlineStart: "auto" }} />}
            </a>
          ))}
        </nav>
        <div className="[ as-main ]">
          {vm.missingGates.length > 0 && (
            <div className="[ banner ]" data-tone="danger" role="status" data-missing-gates="">
              <div className="[ flow ] [ flow-space-3xs grow ]">
                {vm.missingGates.map((g) => (
                  <span key={g.kind}>
                    <span className="[ status ]" data-state="missing">
                      {g.label} missing
                    </span>{" "}
                    · {g.reason === "" ? "no command at any level" : g.reason} · gated sessions on {vm.scopeName} cannot pass until a command is set below or at project level.{" "}
                    <span className="[ code ]">{`REQUIRED_GATE_MISSING · guard.gates.${g.kind}.command`}</span>
                  </span>
                ))}
              </div>
            </div>
          )}
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
        {layer.gates.length > 0 && (
          <>
            <div className="[ ledger-sub ]">
              Gates <span>· required gates cannot be turned off, only their command changed</span>
            </div>
            {layer.gates.map((gate) => (
              <GateRowView key={gate.kind} gate={gate} presenter={presenter} />
            ))}
            <div className="[ ledger-sub ]">Enforcement</div>
          </>
        )}
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
        {row.kind === "toggle" && (
          <button type="button" className="[ switch ]" role="switch" aria-checked={row.value} aria-label={row.label} aria-invalid={row.error ? true : undefined} onClick={() => presenter.edit(row.path, !row.value)}>
            <span className="[ track ]" />
            <span>{row.value ? row.words[1] : row.words[0]}</span>
          </button>
        )}
        {row.kind === "number" && (
          <>
            <input className="[ narrow ]" inputMode="numeric" aria-label={row.label} aria-invalid={row.error ? true : undefined} value={String(row.current)} onChange={(e) => presenter.editNumber(row.path, e.target.value)} />
            <span className="[ meta ]">{row.unit}</span>
          </>
        )}
        {row.kind === "chips" && <Chips row={row} presenter={presenter} />}
        {row.kind === "fixed" && <span className="[ color-ink-muted ]">{row.text}</span>}
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

/** Glob chips with an add field: Enter adds, the × of a chip removes it. */
function Chips({ row, presenter }: { row: Row; presenter: AgentSystemPresenter }) {
  const [adding, setAdding] = useState("");
  const add = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter") return;
    event.preventDefault();
    const glob = adding.trim();
    if (glob === "") return;
    if (!row.items.includes(glob)) presenter.edit(row.path, [...row.items, glob]);
    setAdding("");
  };
  return (
    <>
      {row.items.map((item) => (
        <span key={item} className="[ badge ] [ glob ]" data-tone={row.invalid.includes(item) ? "danger" : undefined}>
          {item}
          <button type="button" className="[ chip-x ]" aria-label={`Remove ${item}`} onClick={() => presenter.edit(row.path, row.items.filter((g) => g !== item))}>
            ×
          </button>
        </span>
      ))}
      <input className="[ chip-add ]" aria-label={`Add to ${row.label}`} placeholder="add a glob" value={adding} onChange={(e) => setAdding(e.target.value)} onKeyDown={add} />
    </>
  );
}

/** One gate kind: its command and timeout inline, a switch only on an optional kind, "always on" on a required one. */
function GateRowView({ gate, presenter }: { gate: GateRow; presenter: AgentSystemPresenter }) {
  const bad = gate.errors.command ?? gate.errors.timeout ?? gate.errors.enabled;
  return (
    <div className="[ ledger-row ]" data-gate={gate.kind} data-missing={gate.missing ? "" : undefined} data-changed={gate.draft ? "" : undefined} data-error={bad ? "" : undefined}>
      <div className="[ lbl ] [ kind ]">
        {gate.missing ? (
          <span className="[ status ]" data-state="missing">
            {gate.label}
          </span>
        ) : (
          gate.label
        )}{" "}
        <span className="[ badge ]">{gate.required ? "required" : "optional"}</span>
      </div>
      <div className="[ ctl ]">
        {!gate.required && (
          <button type="button" className="[ switch ]" role="switch" aria-checked={gate.on} aria-label={`${gate.label} gate`} onClick={() => presenter.edit(gate.paths.enabled, !gate.on)}>
            <span className="[ track ]" />
            <span>{gate.on ? "On" : "Off"}</span>
          </button>
        )}
        <input className="[ mono ] [ command ]" aria-label={`${gate.label} command`} aria-invalid={gate.errors.command ? true : undefined} placeholder={gate.placeholder} value={gate.command} onChange={(e) => presenter.edit(gate.paths.command, e.target.value)} />
        <span className="[ tmo ]">
          <span className="[ meta ]">timeout</span>
          <input className="[ mono ] [ narrow ]" inputMode="numeric" aria-label={`${gate.label} timeout in seconds`} aria-invalid={gate.errors.timeout ? true : undefined} value={String(gate.timeout)} onChange={(e) => presenter.editNumber(gate.paths.timeout, e.target.value)} />
          <span className="[ meta ]">s</span>
        </span>
        {[gate.errors.command, gate.errors.timeout, gate.errors.enabled].map(
          (error) =>
            error && (
              <span key={error.field} className="[ err ]" role="alert" data-error-for={error.field}>
                <code>{error.code}</code> · {error.message}
              </span>
            ),
        )}
      </div>
      <div className="[ src ]">
        <span className="[ origin ]" data-kind={gate.source.kind}>
          {gate.source.text}
        </span>
        {gate.source.reason !== "" && <span>{gate.source.reason}</span>}
        {gate.source.replaces !== "" && <span>{gate.source.replaces}</span>}
      </div>
      <div className="[ act ]">
        {gate.required && <span className="[ lock ]">always on</span>}
        {gate.action === "override" && (
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={() => presenter.overrideGate(gate.kind)}>
            Override
          </button>
        )}
        {gate.action === "reset" && (
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={() => presenter.resetGate(gate.kind)}>
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
