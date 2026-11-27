import { useEffect, useState, type ChangeEvent, type KeyboardEvent } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { GateRow, Layer, Row, VisualQaBlock } from "../../domain/ledger";
import type { RunCommand } from "../../domain/settings";
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
          {vm.status === "ready" && vm.layers.map((layer) => <LayerSection key={layer.key} layer={layer} presenter={presenter} skillsHref={vm.skillsHref} runCommands={vm.runCommands} canImport={vm.canImportCheckSh} importNote={vm.importNote} />)}
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

function LayerSection({ layer, presenter, skillsHref, runCommands, canImport, importNote }: { layer: Layer; presenter: AgentSystemPresenter; skillsHref: string; runCommands: readonly RunCommand[]; canImport: boolean; importNote: string }) {
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
              {canImport && (
                <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-import-check-sh="" onClick={() => void presenter.importFromCheckSh()}>
                  Import from check.sh
                </button>
              )}
            </div>
            {importNote !== "" && (
              <p className="[ pane-note ]" role="status" data-import-note="">
                {importNote}
              </p>
            )}
            {layer.gates.map((gate) => (
              <GateRowView key={gate.kind} gate={gate} presenter={presenter} />
            ))}
            <div className="[ ledger-sub ]">Enforcement</div>
          </>
        )}
        {layer.rows.map((row) => (
          <LedgerRow key={row.label} row={row} presenter={presenter} />
        ))}
        {layer.visualQa !== undefined && <VisualQaView block={layer.visualQa} presenter={presenter} runCommands={runCommands} />}
        {layer.link !== undefined && (
          <div className="[ ledger-row ]" data-note="" data-link="score">
            <div className="[ lbl ]">{layer.link.label}</div>
            <div className="[ ctl ]">
              <a href={layer.link.href}>{layer.link.text} ↗</a>
            </div>
            <div className="[ src ]" />
            <div className="[ act ]" />
          </div>
        )}
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
        {row.kind === "select" && (
          <select data-size="sm" data-fit aria-label={row.label} aria-invalid={row.error ? true : undefined} value={row.selected} onChange={(e) => presenter.selectModel(e.target.value)}>
            {row.options.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
            {row.selected !== "" && !row.options.some((o) => o.value === row.selected) && <option value={row.selected}>{row.selected}</option>}
          </select>
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

/** Chips with an add field: Enter adds, the × of a chip removes it. */
function ChipList({ items, invalid, label, placeholder, normalize, onChange }: { items: readonly string[]; invalid: readonly string[]; label: string; placeholder: string; normalize?: (text: string) => string; onChange: (items: string[]) => void }) {
  const [adding, setAdding] = useState("");
  const add = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter") return;
    event.preventDefault();
    const entry = (normalize ? normalize(adding) : adding).trim();
    if (entry === "") return;
    if (!items.includes(entry)) onChange([...items, entry]);
    setAdding("");
  };
  return (
    <>
      {items.map((item) => (
        <span key={item} className="[ badge ] [ glob ]" data-tone={invalid.includes(item) ? "danger" : undefined}>
          {item}
          <button type="button" className="[ chip-x ]" aria-label={`Remove ${item}`} onClick={() => onChange(items.filter((g) => g !== item))}>
            ×
          </button>
        </span>
      ))}
      <input className="[ chip-add ]" aria-label={`Add to ${label}`} placeholder={placeholder} value={adding} onChange={(e) => setAdding(e.target.value)} onKeyDown={add} />
    </>
  );
}

function Chips({ row, presenter }: { row: Row; presenter: AgentSystemPresenter }) {
  return <ChipList items={row.items} invalid={row.invalid} label={row.label} placeholder={row.placeholder === "" ? "add a glob" : row.placeholder} onChange={(items) => presenter.edit(row.path, items)} />;
}

const CUSTOM = "__custom__";

/** The visual QA UI declaration: one block, added and removed as a whole; its fields are rows of the ledger. */
function VisualQaView({ block, presenter, runCommands }: { block: VisualQaBlock; presenter: AgentSystemPresenter; runCommands: readonly RunCommand[] }) {
  const ui = block.ui;
  const known = runCommands.some((c) => c.command === ui.start_command);
  const [custom, setCustom] = useState(runCommands.length === 0 || (ui.start_command !== "" && !known));
  const [routes, setRoutes] = useState(ui.routes.join("\n"));
  // The draft changed from elsewhere (Discard, Reset): the text follows it.
  useEffect(() => {
    setRoutes((text) => (parseRoutes(text).join("\n") === ui.routes.join("\n") ? text : ui.routes.join("\n")));
  }, [ui.routes]);
  const mark = <span className="[ origin ]" data-kind={block.source.kind}>{block.source.text}</span>;
  return (
    <>
      <div className="[ ledger-sub ]" data-visual-qa={block.declared ? "declared" : "none"}>
        Visual QA <span>· on demand, from the agent box or by the architect</span>
        {block.declared && (
          <>
            {mark}
            <span style={{ marginInlineStart: "auto" }}>
              <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={() => presenter.removeVisualQa()}>
                Remove declaration
              </button>
            </span>
          </>
        )}
      </div>
      {!block.declared && (
        <div className="[ ledger-row ]" data-path={block.path}>
          <div className="[ lbl ]">
            UI declaration
            <small>Per repository. The project holds no default: a repository without one is skipped.</small>
          </div>
          <div className="[ ctl ] [ color-ink-muted ]">
            {block.canDeclare ? (
              <button type="button" className="[ button ]" data-size="sm" onClick={() => presenter.declareVisualQa()}>
                Declare this repository's UI
              </button>
            ) : (
              "Choose a repository to declare its UI: start command, URL, routes, viewports, UI paths."
            )}
          </div>
          <div className="[ src ]" />
          <div className="[ act ]" />
        </div>
      )}
      {block.declared && (
        <>
          {block.error && (
            <div className="[ ledger-row ]" data-error="" data-path={block.path}>
              <div className="[ lbl ]" />
              <div className="[ ctl ]">
                <span className="[ err ]" role="alert" data-error-for={block.path}>
                  <code>{block.error.code}</code> · {block.error.message}
                </span>
              </div>
              <div className="[ src ]" />
              <div className="[ act ]" />
            </div>
          )}
          <div className="[ ledger-row ]" data-visual-qa-field="start_command">
            <div className="[ lbl ]">
              Start command
              <small>A run command of this repository, started as a task service the person approves.</small>
            </div>
            <div className="[ ctl ]">
              {runCommands.length > 0 && (
                <select
                  data-size="sm"
                  data-fit
                  aria-label="Start command"
                  value={custom ? CUSTOM : known ? ui.start_command : ""}
                  onChange={(e) => {
                    if (e.target.value === CUSTOM) setCustom(true);
                    else {
                      setCustom(false);
                      presenter.editVisualQa({ start_command: e.target.value });
                    }
                  }}
                >
                  {!custom && !known && <option value="">Choose a command</option>}
                  {runCommands.map((c) => (
                    <option key={c.name} value={c.command}>{`${c.name} (${c.command})`}</option>
                  ))}
                  <option value={CUSTOM}>custom…</option>
                </select>
              )}
              {custom && <input className="[ mono ] [ command ]" aria-label="Custom start command" value={ui.start_command} onChange={(e) => presenter.editVisualQa({ start_command: e.target.value })} />}
            </div>
            <div className="[ src ]" />
            <div className="[ act ]" />
          </div>
          <div className="[ ledger-row ]" data-visual-qa-field="url">
            <div className="[ lbl ]">URL</div>
            <div className="[ ctl ]">
              <input className="[ mono ] [ command ]" aria-label="URL" value={ui.url} onChange={(e) => presenter.editVisualQa({ url: e.target.value })} />
            </div>
            <div className="[ src ]" />
            <div className="[ act ]" />
          </div>
          <div className="[ ledger-row ]" data-visual-qa-field="routes">
            <div className="[ lbl ]">
              Routes
              <small>One per line. Relative to the URL.</small>
            </div>
            <div className="[ ctl ]">
              <textarea
                className="[ mono ]"
                aria-label="Routes"
                aria-invalid={block.error?.message.includes("line") ? true : undefined}
                rows={Math.max(3, ui.routes.length + 1)}
                value={routes}
                onChange={(e) => {
                  setRoutes(e.target.value);
                  presenter.editVisualQa({ routes: parseRoutes(e.target.value) });
                }}
              />
            </div>
            <div className="[ src ]" />
            <div className="[ act ]" />
          </div>
          <div className="[ ledger-row ]" data-visual-qa-field="viewports">
            <div className="[ lbl ]">
              Viewports
              <small>Width × height in pixels.</small>
            </div>
            <div className="[ ctl ]">
              <ChipList items={ui.viewports} invalid={[]} label="Viewports" placeholder="w×h" normalize={(text) => text.replace(/\s*[×x]\s*/i, "x")} onChange={(viewports) => presenter.editVisualQa({ viewports })} />
            </div>
            <div className="[ src ]" />
            <div className="[ act ]" />
          </div>
          <div className="[ ledger-row ]" data-visual-qa-field="ui_paths">
            <div className="[ lbl ]">
              UI paths
              <small>Globs whose change makes a diff worth a visual pass.</small>
            </div>
            <div className="[ ctl ]">
              <ChipList items={ui.ui_paths} invalid={[]} label="UI paths" placeholder="add a glob" onChange={(ui_paths) => presenter.editVisualQa({ ui_paths })} />
            </div>
            <div className="[ src ]" />
            <div className="[ act ]" />
          </div>
        </>
      )}
    </>
  );
}

/** The routes as typed: one per line, blank lines left out. */
function parseRoutes(text: string): string[] {
  return text.split("\n").map((line) => line.trim()).filter((line) => line !== "");
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
