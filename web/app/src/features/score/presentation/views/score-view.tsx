import { usePresenter } from "../../../../shared/presentation/use-presenter";
import { FILTERS, type CaseVm, type Filter, type FormErrorKey, type FormVm, type RowVm, type RunVm, type ScorePresenter, type ScoreViewModel, type TileVm, type TrajectoriesVm, type View } from "../presenters/score-presenter";

const VIEWS: readonly { key: View; label: string }[] = [
  { key: "scorecard", label: "Scorecard" },
  { key: "cases", label: "Cases" },
  { key: "runs", label: "Runs" },
];
const FILTER_LABEL: Record<Filter, string> = { all: "All", regressed: "Regressed", improved: "Improved", failed: "Failed", new: "New" };

/** The page: the toolbar with the view switch and Run bench, then the Scorecard, Cases or Runs view. */
export function ScoreView({ presenter }: { presenter: ScorePresenter }) {
  const vm = usePresenter(presenter);
  const count = (key: View): number | null => (key === "cases" ? vm.caseCount : key === "runs" ? vm.runCount : null);
  return (
    <>
      <header className="[ toolbar ]">
        <a className="[ crumb ]" href={`/projects/${encodeURIComponent(vm.projectId)}`}>
          {vm.projectName} /
        </a>
        <h1>Score</h1>
        <div className="[ segmented ]" role="group" aria-label="View">
          {VIEWS.map((v) => (
            <button key={v.key} type="button" aria-pressed={vm.view === v.key} onClick={() => presenter.setView(v.key)}>
              {v.label}
              {count(v.key) !== null && <span className="[ count ]"> {count(v.key)}</span>}
            </button>
          ))}
        </div>
        <span className="[ end ]">
          <span className="[ color-ink-faint text-sm ]" role="status">
            {vm.running ? `running · ${vm.running.done} of ${vm.running.total} cases` : vm.lastRun}
          </span>
          <button type="button" className="[ button ]" data-variant="primary" aria-busy={vm.running !== null} disabled={vm.status !== "ready" || vm.caseCount === 0 || vm.running !== null} onClick={() => void presenter.runBench()}>
            Run bench
          </button>
        </span>
      </header>
      <div className="[ score ]" data-scroll>
        {vm.problem !== "" && (
          <div className="[ banner ]" data-tone="danger" role="alert" data-problem="">
            <div className="[ flow ] [ flow-space-3xs grow ]">
              <span>Run bench did not start.</span>
              <span className="[ code ]">{vm.problem}</span>
            </div>
          </div>
        )}
        {vm.status === "loading" && <Skeleton />}
        {vm.status === "failed" && (
          <div className="[ banner ]" data-tone="danger" role="alert">
            <div className="[ flow ] [ flow-space-3xs grow ]">
              <span>The score could not be read.</span>
              <span className="[ code ]">{vm.error}</span>
            </div>
            <button type="button" className="[ button ]" data-size="sm" onClick={() => void presenter.retry()}>
              Retry
            </button>
          </div>
        )}
        {vm.status === "ready" && vm.view === "scorecard" && <Scorecard vm={vm} presenter={presenter} />}
        {vm.status === "ready" && vm.view === "cases" && <Cases vm={vm} presenter={presenter} />}
        {vm.status === "ready" && vm.view === "runs" && <Runs runs={vm.runs} presenter={presenter} />}
      </div>
    </>
  );
}

function Skeleton() {
  return (
    <div className="[ score-tiles ]" aria-busy="true" data-skeleton="">
      {[0, 1, 2, 3, 4, 5].map((i) => (
        <div key={i} className="[ score-tile ]">
          <span className="[ skeleton ]" />
        </div>
      ))}
    </div>
  );
}

function Scorecard({ vm, presenter }: { vm: ScoreViewModel; presenter: ScorePresenter }) {
  if (vm.state === "no-cases") {
    return (
      <div className="[ empty ]" data-state="no-cases">
        <h2>No bench cases yet</h2>
        <p>A case is a prompt with mock tools and an expectation, or a recorded session. Add one by hand, or turn on recording on a task and make a case from one of its sessions.</p>
      </div>
    );
  }
  return (
    <>
      {vm.state === "no-run" ? (
        <p className="[ color-ink-muted text-sm ]" data-notice="">
          {vm.notice}
        </p>
      ) : (
        <div className="[ score-compare ]">
          <span className="[ cluster ] [ text-sm ]">
            <span>Comparing</span>
            <span className="[ segmented ]">
              <button type="button" aria-pressed="true" disabled>
                {vm.readName}
              </button>
            </span>
            <span className="[ color-ink-muted ]">with</span>
            {vm.state === "first-run" ? (
              <span className="[ color-ink-faint ]" data-baseline="">
                nothing yet · first run
              </span>
            ) : (
              <select data-size="sm" data-fit aria-label="Baseline" data-baseline="" value={vm.runs.find((r) => r.baseline)?.id ?? ""} onChange={(e) => presenter.selectBaseline(e.target.value === "" ? null : e.target.value)}>
                {vm.runs
                  .filter((r) => !r.reading)
                  .map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name} · {r.when}
                      {r.partial ? " · partial" : ""}
                    </option>
                  ))}
              </select>
            )}
          </span>
          {vm.fingerprint && (
            <span className="[ score-fp ]">
              setup <b>{vm.fingerprint.short}</b>
              {vm.fingerprint.parts !== "" && ` · ${vm.fingerprint.parts}`}
              {vm.fingerprint.changed !== "" && <span className="[ chg ]"> · changed since {vm.baselineName.split(" · ")[0]}: {vm.fingerprint.changed}</span>}
            </span>
          )}
        </div>
      )}
      {vm.noise !== "" && (
        <div className="[ banner ]" data-tone="attention" role="status" data-noise="">
          <div>{vm.noise}</div>
        </div>
      )}
      {vm.tiles.length > 0 && (
        <div className="[ score-tiles ]">
          {vm.tiles.map((t) => (
            <Tile key={t.key} tile={t} />
          ))}
        </div>
      )}
      {vm.state !== "no-run" && (
        <>
          <div className="[ repel ] [ score-filter ]">
            <div className="[ segmented ]" role="group" aria-label="Filter">
              {FILTERS.map((f) => (
                <button key={f} type="button" aria-pressed={vm.filter === f} onClick={() => presenter.setFilter(f)}>
                  {FILTER_LABEL[f]} <span className="[ count ]">{vm.filterCounts[f]}</span>
                </button>
              ))}
            </div>
            <span className="[ color-ink-faint text-xs ]">scores 0–1 · a case fails under 0.6 · judged by the LLM judge on the trace and the outcome</span>
          </div>
          <div className="[ score-list ]" role="table" aria-label="Cases">
            <div className="[ sc-row ]" data-head="" role="row">
              <span>Case</span>
              <span>Pattern</span>
              <span className="[ num ]">tool use</span>
              <span className="[ num ]">outcome</span>
              <span className="[ num ]">trajectory</span>
              <span className="[ num ]">{vm.readName.split(" · ")[0]}</span>
              <span className="[ delta ]">{vm.baselineName === "" ? "" : `vs ${vm.baselineName.split(" · ")[0]?.replace("run ", "")}`}</span>
            </div>
            {vm.rows.map((r) => (
              <CaseRowView key={r.caseId} row={r} />
            ))}
            {vm.later.map((label) => (
              <div key={label} className="[ sc-row ]" data-muted="" data-later="">
                <span className="[ title ]">{label.split(" · ")[0]}</span>
                <span className="[ meta ]">{label}</span>
                <span className="[ num ]">—</span>
                <span className="[ num ]">—</span>
                <span className="[ num ]">—</span>
                <span className="[ num ]">—</span>
                <span className="[ delta ]">no cases yet</span>
              </div>
            ))}
          </div>
        </>
      )}
    </>
  );
}

function Tile({ tile }: { tile: TileVm }) {
  return (
    <div className="[ score-tile ]" data-tile={tile.key}>
      <div className="[ label ]">{tile.label}</div>
      <div className="[ value ]">{tile.value}</div>
      <div className="[ delta ]" data-dir={tile.direction}>
        {tile.note}
      </div>
    </div>
  );
}

function CaseRowView({ row }: { row: RowVm }) {
  return (
    <div className="[ sc-row ]" role="row" data-regressed={row.regressed ? "" : undefined} data-failed={row.failed ? "" : undefined} data-case={row.caseId}>
      <span className="[ title ]" title={row.error === "" ? row.title : `${row.title} · ${row.error}`}>
        {row.title}
      </span>
      <span className="[ meta ]">{row.pattern}</span>
      <span className="[ num ]">{row.toolUse}</span>
      <span className="[ num ]">{row.outcome}</span>
      <span className="[ num ]">{row.trajectory}</span>
      <span className="[ num ] [ now ]">{row.now}</span>
      <span className="[ delta ]" data-dir={row.direction}>
        {row.delta}
      </span>
    </div>
  );
}

/** The Cases view: each case opens in place to its trace; Add case and From a recorded trajectory sit above the list. */
function Cases({ vm, presenter }: { vm: ScoreViewModel; presenter: ScorePresenter }) {
  return (
    <>
      <div className="[ repel ]">
        <span className="[ color-ink-faint text-xs ]">{vm.caseCount} {vm.caseCount === 1 ? "case" : "cases"} · a run scores every one</span>
        <span className="[ cluster ]">
          <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-action="from-trajectory" onClick={() => void presenter.openTrajectories()}>
            From a recorded trajectory
          </button>
          <button type="button" className="[ button ]" data-size="sm" data-action="add-case" onClick={() => presenter.startAdd()}>
            Add case
          </button>
        </span>
      </div>
      {vm.form && <CaseForm form={vm.form} presenter={presenter} />}
      {vm.trajectories && <Trajectories t={vm.trajectories} presenter={presenter} />}
      {vm.cases.length === 0 ? (
        <div className="[ empty ]" data-state="no-cases">
          <h2>No bench cases yet</h2>
          <p>A case is a prompt with mock tools and an expectation, or a recorded session. Add one by hand, or turn on recording on a task and make a case from one of its sessions.</p>
        </div>
      ) : (
        <div className="[ score-list ]" role="table" aria-label="Bench cases">
          {vm.cases.map((c) => (
            <CaseBlock key={c.id} c={c} presenter={presenter} />
          ))}
        </div>
      )}
    </>
  );
}

function Spark({ trend }: { trend: readonly number[] }) {
  if (trend.length === 0) return <span className="[ meta ]">—</span>;
  return (
    <span className="[ spark ]" role="img" aria-label={`last ${trend.length} runs: ${trend.map((n) => n.toFixed(2)).join(", ")}`}>
      {trend.map((n, i) => (
        <i key={i} style={{ height: `${Math.max(2, Math.round(n * 14))}px` }} />
      ))}
    </span>
  );
}

function CaseBlock({ c, presenter }: { c: CaseVm; presenter: ScorePresenter }) {
  const open = c.trace !== null;
  return (
    <>
      <button type="button" className="[ sc-row ]" data-kind="case" role="row" data-case={c.id} data-open={open ? "" : undefined} aria-expanded={open} onClick={() => presenter.openCase(c.id)}>
        <span className="[ title ]">
          {c.title}
          {c.source === "recorded" && <span className="[ badge ]"> recorded</span>}
        </span>
        <span className="[ meta ]">{c.pattern || "—"}</span>
        <span className="[ meta ]">{c.tools}</span>
        <Spark trend={c.trend} />
        <span className="[ num ] [ now ]">{c.score}</span>
      </button>
      {c.trace && (
        <div className="[ case-detail ]" data-detail={c.id}>
          <div className="[ repel ]">
            <span className="[ muted text-sm ]">{c.trace.now === "—" ? "not scored in the run read" : `score ${c.trace.now}${c.trace.baseline === "—" ? "" : ` · baseline ${c.trace.baseline}`}`}</span>
            <span className="[ cluster ]">
              <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-action="edit-expectation" onClick={() => presenter.startEdit(c.id)}>
                Edit expectation
              </button>
              {c.confirmingDelete ? (
                <span className="[ cluster ]" role="group" aria-label="Delete this case?">
                  <span className="[ text-sm ]">Delete this case and its history?</span>
                  <button type="button" className="[ button ]" data-variant="danger" data-size="sm" data-action="confirm-delete" onClick={() => void presenter.confirmDelete(c.id)}>
                    Delete
                  </button>
                  <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={() => presenter.cancelDelete()}>
                    Keep
                  </button>
                </span>
              ) : (
                <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" data-action="delete" onClick={() => presenter.requestDelete(c.id)}>
                  Delete
                </button>
              )}
            </span>
          </div>
          <p className="[ text-sm ]">
            <b>Prompt:</b> {c.trace.prompt}
          </p>
          <p className="[ text-sm ]" data-expectation="">
            <b>Expected:</b> {c.trace.expectation.calls === "" ? "no tool calls" : c.trace.expectation.calls.split("\n").join(c.trace.expectation.ordered ? " → " : " · ")}
            {c.trace.expectation.reference !== "" && <> · result like “{c.trace.expectation.reference}”</>}
          </p>
          {c.trace.steps.length > 0 && (
            <ol className="[ trace ] [ plain ]" aria-label="Trace">
              {c.trace.steps.map((s) => (
                <li key={s.n} data-bad={s.failed ? "" : undefined}>
                  <span>{s.n}</span>
                  <span>{s.call === "" ? "(no call)" : s.call}</span>
                  <span className="[ j ]">{s.note}</span>
                </li>
              ))}
            </ol>
          )}
          <p className="[ text-sm muted ]">
            <b>Judge:</b> {c.trace.judge}
          </p>
          {c.trace.finalText !== "" && (
            <p className="[ text-sm muted ]">
              <b>Final text:</b> {c.trace.finalText}
            </p>
          )}
        </div>
      )}
    </>
  );
}

function Field({ label, error, children }: { label: string; error: string | undefined; children: React.ReactNode }) {
  return (
    <label className="[ case-field ]">
      <span className="[ text-sm ]">{label}</span>
      {children}
      {error && (
        <span className="[ error ]" role="alert">
          <span aria-hidden="true">✕</span> {error}
        </span>
      )}
    </label>
  );
}

function CaseForm({ form, presenter }: { form: FormVm; presenter: ScorePresenter }) {
  const bad = (key: FormErrorKey): true | undefined => (form.errors[key] ? true : undefined);
  const add = form.mode === "add";
  return (
    <form
      className="[ case-form ]"
      data-form={form.mode}
      onSubmit={(e) => {
        e.preventDefault();
        void presenter.saveForm();
      }}
    >
      <h2 className="[ text-md ]">{form.title}</h2>
      {form.errors.general && (
        <div className="[ banner ]" data-tone="danger" role="alert">
          <div>{form.errors.general}</div>
        </div>
      )}
      {add && (
        <>
          <Field label="Name" error={form.errors.name}>
            <input type="text" name="name" value={form.name} aria-invalid={bad("name")} onChange={(e) => presenter.setForm({ name: e.target.value })} />
          </Field>
          <Field label="Prompt" error={form.errors.prompt}>
            <textarea name="prompt" rows={3} value={form.prompt} aria-invalid={bad("prompt")} onChange={(e) => presenter.setForm({ prompt: e.target.value })} />
          </Field>
          <Field label="Mock tools, one per line: name = canned result, or name ! error result" error={form.errors.mocks}>
            <textarea name="mocks" rows={3} className="[ mono ]" value={form.mocks} aria-invalid={bad("mocks")} onChange={(e) => presenter.setForm({ mocks: e.target.value })} />
          </Field>
        </>
      )}
      <Field label={'Expected calls, one per line: tool, or tool {"param": value}'} error={form.errors.calls}>
        <textarea name="calls" rows={3} className="[ mono ]" value={form.calls} aria-invalid={bad("calls")} onChange={(e) => presenter.setForm({ calls: e.target.value })} />
      </Field>
      <label className="[ cluster text-sm ]">
        <input type="checkbox" name="ordered" checked={form.ordered} onChange={(e) => presenter.setForm({ ordered: e.target.checked })} />
        <span>In this order</span>
      </label>
      <Field label="Reference result (what a good final answer says)" error={form.errors.reference}>
        <textarea name="reference" rows={2} value={form.reference} aria-invalid={bad("reference")} onChange={(e) => presenter.setForm({ reference: e.target.value })} />
      </Field>
      <span className="[ cluster ]">
        <button type="submit" className="[ button ]" data-variant="primary" data-size="sm" aria-busy={form.saving} disabled={form.saving}>
          {add ? "Add case" : "Save expectation"}
        </button>
        <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={() => presenter.cancelForm()}>
          Cancel
        </button>
      </span>
    </form>
  );
}

function Trajectories({ t, presenter }: { t: TrajectoriesVm; presenter: ScorePresenter }) {
  return (
    <section className="[ case-form ]" data-trajectories="" aria-label="Recorded trajectories">
      <div className="[ repel ]">
        <h2 className="[ text-md ]">From a recorded trajectory</h2>
        <button type="button" className="[ button ]" data-variant="ghost" data-size="sm" onClick={() => presenter.closeTrajectories()}>
          Close
        </button>
      </div>
      {t.error !== "" && (
        <div className="[ banner ]" data-tone="danger" role="alert">
          <div>{t.error}</div>
        </div>
      )}
      {t.loading ? (
        <p className="[ color-ink-muted text-sm ]">Reading the recordings…</p>
      ) : t.items.length === 0 ? (
        <p className="[ color-ink-muted text-sm ]" data-empty="">
          {t.empty}
        </p>
      ) : (
        <ul className="[ plain ]">
          {t.items.map((x) => (
            <li key={x.id} className="[ repel ]" data-trajectory={x.id}>
              <span className="[ text-sm ]">
                {x.prompt.slice(0, 120)} <span className="[ color-ink-faint text-xs ]">· {x.steps} · {x.when}</span>
              </span>
              <button type="button" className="[ button ]" data-size="sm" onClick={() => void presenter.chooseTrajectory(x.id)}>
                Make a case
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Runs({ runs, presenter }: { runs: readonly RunVm[]; presenter: ScorePresenter }) {
  if (runs.length === 0) return <p className="[ color-ink-muted text-sm ]">No run yet.</p>;
  return (
    <div className="[ score-list ]" role="table" aria-label="Runs">
      {runs.map((r) => (
        <button key={r.id} type="button" className="[ sc-row ]" data-kind="run" role="row" data-run={r.id} data-partial={r.partial ? "" : undefined} aria-current={r.reading ? "true" : undefined} onClick={() => presenter.selectRead(r.id)}>
          <span className="[ title ]">
            {r.name} · {r.when}
          </span>
          <span className="[ meta ]">{r.status}</span>
          <span className="[ num ]">{r.score}</span>
          <span className="[ num ]">{r.cost}</span>
          <span className="[ num ]">{r.duration}</span>
        </button>
      ))}
    </div>
  );
}
