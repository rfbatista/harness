import { useEffect, useState, type ChangeEvent, type KeyboardEvent, type MouseEvent } from "react";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { GateRow, Layer, Row, VisualQaBlock } from "../../domain/ledger";
import type { RunCommand } from "../../domain/settings";
import type { AgentSystemPresenter, AgentSystemViewModel, ErrorEntry, OverviewEntry } from "../presenters/agent-system-presenter";

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
          {projectScope ? (
            <button type="button" aria-pressed="false" disabled={vm.repositories.length === 0} onClick={() => vm.repositories[0] && void presenter.selectScope(vm.repositories[0].id)}>
              Repository <span className="[ count ]">{vm.repositories.length}</span>
            </button>
          ) : (
            <span className="[ scope-pick ]" data-pressed="">
              {chosen?.red && <span className="[ status ]" data-state="missing" data-dot-only role="img" aria-label="missing a required gate" />}
              <select aria-label="Repository" value={vm.scope.kind === "repository" ? vm.scope.id : ""} onChange={onScope}>
                {vm.repositories.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.red ? `${r.name} · missing a required gate` : r.name}
                  </option>
                ))}
              </select>
            </span>
          )}
        </div>
        <span className="[ end ] [ color-ink-faint text-sm ]" role="status">
          {vm.saved || vm.summary}
        </span>
      </header>
      <div className="[ agent-system ]" data-scroll>
        <nav className="[ as-index ]" aria-label="Sections">
          {vm.layers.map((layer) => (
            <a key={layer.key} href={`#as-${layer.key}`}>
              {layer.title}
              {layer.attention > 0 && <span className="[ status ]" data-state="missing" data-dot-only role="img" aria-label={vm.scope.kind === "project" ? `${layer.attention} red ${layer.attention === 1 ? "repository" : "repositories"}` : `${layer.attention} missing`} style={{ marginInlineStart: "auto" }} />}
              {layer.overrides > 0 && (
                <span className="[ meta ]" data-overrides-count="" style={{ marginInlineStart: "auto" }}>
                  {layer.overrides} {layer.overrides === 1 ? "override" : "overrides"}
                </span>
              )}
            </a>
          ))}
          {vm.scope.kind === "repository" && vm.status === "ready" && (
            <>
              <div className="[ sum ]" data-own-overrides="">
                <b>Overrides on {vm.scopeName}</b>
                {vm.ownOverrides.length === 0 && <span>None</span>}
                {vm.ownOverrides.map((o) => (
                  <span key={o}>{o}</span>
                ))}
              </div>
              <div className="[ sum ]" data-also-here="">
                <b>Also on this repository</b>
                {vm.alsoHere.map((l) => (
                  <a key={l.href} href={l.href}>
                    {l.text} ↗
                  </a>
                ))}
              </div>
            </>
          )}
          {vm.scope.kind === "project" && <Overview presenter={presenter} overview={vm.overview} status={vm.status} />}
        </nav>
        <div className="[ as-main ]">
          {vm.changedElsewhere && (
            <div className="[ banner ]" data-tone="attention" role="status" data-changed-elsewhere="">
              <div className="[ flow ] [ flow-space-3xs grow ]">
                <span>
                  These settings changed on another page while you were editing. <span className="[ code ]">agent_system_settings</span> Reload keeps your unsaved rows and refreshes the rest.
                </span>
              </div>
              <button type="button" className="[ button ]" data-size="sm" onClick={() => void presenter.reload()}>
                Reload
              </button>
            </div>
          )}
          {vm.status === "ready" && vm.overview.newProject && (
            <div className="[ banner ]" role="status" data-new-project="">
              <div>Nothing set yet. Every value below is a harness default, the gates detected from each repository's stack. The layers already run on these; change what is wrong for this project.</div>
            </div>
          )}
          {vm.missingGates.length > 0 && (
            <div className="[ red-line ]" role="status" data-missing-gates="">
              {vm.missingGates.map((g) => (
                <p key={g.kind}>
                  <span className="[ status ]" data-state="missing">
                    {g.label} missing
                  </span>{" "}
                  <span className="[ color-ink-muted ]">
                    · {g.reason === "" ? "no command at any level" : g.reason} · gated sessions on {vm.scopeName} end not passed until a command is set below or at project level
                  </span>
                </p>
              ))}
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
          {vm.status === "ready" && vm.layers.map((layer) => <LayerSection key={layer.key} scopeIsRepository={vm.scope.kind === "repository"} layer={layer} presenter={presenter} skillsHref={vm.skillsHref} runCommands={vm.runCommands} canImport={vm.canImportCheckSh} importNote={vm.importNote} />)}
          {vm.status === "ready" && <SaveBar presenter={presenter} unsaved={vm.unsaved} errors={vm.errors} canSave={vm.canSave} saving={vm.saving} label={vm.saveLabel} />}
        </div>
      </div>
    </>
  );
}

/** What each repository overrides and which are red: links that open that repository's scope. */
function Overview({ presenter, overview, status }: { presenter: AgentSystemPresenter; overview: AgentSystemViewModel["overview"]; status: string }) {
  if (status !== "ready") return null;
  const open = (id: string) => (event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    void presenter.selectScope(id);
  };
  const link = (e: OverviewEntry) => (
    <a href={`?repository=${encodeURIComponent(e.repositoryId)}`} onClick={open(e.repositoryId)}>
      {e.name}
    </a>
  );
  return (
    <>
      <div className="[ sum ]" id="as-overrides" data-overrides="">
        <b>Overrides</b>
        {overview.overrides.length === 0 && <span>No overrides</span>}
        {overview.overrides.map((e) => (
          <span key={e.repositoryId} title={e.fields.join(", ")}>
            {link(e)} · {e.fields.length} {e.fields.length === 1 ? "field" : "fields"}
          </span>
        ))}
      </div>
      <div className="[ sum ]" id="as-red" data-red-repositories="">
        <b>Red repositories</b>
        {overview.red.length === 0 && <span>None</span>}
        {overview.red.map((e) => (
          <span key={e.repositoryId}>
            {link(e)} · {e.missing.join(", ").toLowerCase()}
          </span>
        ))}
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

function LayerSection({ layer, presenter, skillsHref, runCommands, canImport, importNote, scopeIsRepository }: { scopeIsRepository: boolean; layer: Layer; presenter: AgentSystemPresenter; skillsHref: string; runCommands: readonly RunCommand[]; canImport: boolean; importNote: string }) {
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
              <GateRowView key={gate.kind} gate={gate} presenter={presenter} repository={scopeIsRepository} />
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
        {row.kind === "select" && <InlineSelect row={row} presenter={presenter} />}
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

/** Chips: plain at rest. Editing (the Edit button, an unsaved change, or `always`) shows the × of each chip and the add field; Enter adds. */
function ChipList({ items, invalid, label, placeholder, normalize, draft, always, onChange }: { items: readonly string[]; invalid: readonly string[]; label: string; placeholder: string; normalize?: (text: string) => string; draft?: boolean; always?: boolean; onChange: (items: string[]) => void }) {
  const [adding, setAdding] = useState("");
  const [open, setOpen] = useState(false);
  const editing = always === true || open || draft === true;
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
          {editing && (
            <button type="button" className="[ chip-x ]" aria-label={`Remove ${item}`} onClick={() => onChange(items.filter((g) => g !== item))}>
              ×
            </button>
          )}
        </span>
      ))}
      {editing ? (
        <input className="[ chip-add ]" aria-label={`Add to ${label}`} placeholder={placeholder} value={adding} onChange={(e) => setAdding(e.target.value)} onKeyDown={add} />
      ) : (
        <button type="button" className="[ button ] [ chip-edit ]" data-variant="ghost" data-size="sm" aria-label={`Edit ${label}`} onClick={() => setOpen(true)}>
          Edit
        </button>
      )}
    </>
  );
}

function Chips({ row, presenter }: { row: Row; presenter: AgentSystemPresenter }) {
  return <ChipList items={row.items} invalid={row.invalid} label={row.label} draft={row.draft} placeholder={row.placeholder === "" ? "add a glob" : row.placeholder} onChange={(items) => presenter.edit(row.path, items)} />;
}

/** A select that reads as inline text at rest ("opus"); pointing to Change opens it. An unsaved change keeps it open. */
function InlineSelect({ row, presenter }: { row: Row; presenter: AgentSystemPresenter }) {
  const [open, setOpen] = useState(false);
  const shown = row.options.find((o) => o.value === row.selected)?.label ?? row.selected;
  if (!open && !row.draft && !row.error) {
    return (
      <>
        <span data-select-text="">{shown}</span>
        <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" aria-label={`Change ${row.label}`} onClick={() => setOpen(true)}>
          Change
        </button>
      </>
    );
  }
  return (
    <select data-size="sm" data-fit autoFocus={open} aria-label={row.label} aria-invalid={row.error ? true : undefined} value={row.selected} onChange={(e) => presenter.selectModel(e.target.value)}>
      {row.options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
      {row.selected !== "" && !row.options.some((o) => o.value === row.selected) && <option value={row.selected}>{row.selected}</option>}
    </select>
  );
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
              <ChipList always items={ui.viewports} invalid={[]} label="Viewports" placeholder="w×h" normalize={(text) => text.replace(/\s*[×x]\s*/i, "x")} onChange={(viewports) => presenter.editVisualQa({ viewports })} />
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
              <ChipList always items={ui.ui_paths} invalid={[]} label="UI paths" placeholder="add a glob" onChange={(ui_paths) => presenter.editVisualQa({ ui_paths })} />
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

/** One gate kind: its command and timeout inline, a switch only on an optional kind (with the timeout on a line below). */
function GateRowView({ gate, presenter, repository }: { gate: GateRow; presenter: AgentSystemPresenter; repository: boolean }) {
  const bad = gate.errors.command ?? gate.errors.timeout ?? gate.errors.enabled;
  const timeout = (
    <span className="[ tmo ]">
      <span className="[ meta ]">timeout</span>
      <input className="[ mono ] [ narrow ]" inputMode="numeric" aria-label={`${gate.label} timeout in seconds`} aria-invalid={gate.errors.timeout ? true : undefined} value={String(gate.timeout)} onChange={(e) => presenter.editNumber(gate.paths.timeout, e.target.value)} />
      <span className="[ meta ]">s</span>
    </span>
  );
  const command = <input className="[ mono ] [ command ]" aria-label={`${gate.label} command`} aria-invalid={gate.errors.command ? true : undefined} placeholder={gate.placeholder} value={gate.command} onChange={(e) => presenter.edit(gate.paths.command, e.target.value)} />;
  return (
    <div className="[ ledger-row ]" data-gate={gate.kind} data-required={gate.required ? "" : undefined} data-missing={gate.missing ? "" : undefined} data-changed={gate.draft ? "" : undefined} data-error={bad ? "" : undefined}>
      <div className="[ lbl ] [ kind ]">
        {gate.missing ? (
          <span className="[ status ]" data-state="missing">
            {gate.label}
          </span>
        ) : (
          gate.label
        )}{" "}
        <span className="[ meta ]" data-gate-kind="">
          {gate.required ? "required" : "optional"}
        </span>
      </div>
      <div className="[ ctl ]">
        {gate.required ? (
          <>
            {command}
            {timeout}
          </>
        ) : (
          <>
            <span className="[ gate-line ]">
              <button type="button" className="[ switch ]" role="switch" aria-checked={gate.on} aria-label={`${gate.label} gate`} onClick={() => presenter.edit(gate.paths.enabled, !gate.on)}>
                <span className="[ track ]" />
                <span>{gate.on ? "On" : "Off"}</span>
              </button>
              {command}
            </span>
            {timeout}
          </>
        )}
        {gate.missing && (
          <span className="[ err ]" role="status" data-missing-help="">
            {gate.source.reason === "" ? "no command at any level" : gate.source.reason} · {gate.kind === "secret_scan" ? "install gitleaks, or set a command this repository can run" : "set a command this repository can run"}
          </span>
        )}
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
        {gate.source.reason !== "" && !gate.missing && <span>{gate.source.reason}</span>}
        {gate.source.replaces !== "" && <span>{gate.source.replaces}</span>}
      </div>
      <div className="[ act ]">
        {gate.required && !repository && <span className="[ lock ]">always on</span>}
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
