import { usePresenter } from "../../../../shared/presentation/use-presenter";
import { FILTERS, type Filter, type RowVm, type RunVm, type ScorePresenter, type ScoreViewModel, type TileVm, type View } from "../presenters/score-presenter";

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
        {vm.status === "ready" && vm.view === "cases" && <Cases vm={vm} />}
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

/** The Cases view: the cases of the project (opening a case in place to its trace comes with the next ticket). */
function Cases({ vm }: { vm: ScoreViewModel }) {
  if (vm.cases.length === 0) {
    return (
      <div className="[ empty ]" data-state="no-cases">
        <h2>No bench cases yet</h2>
        <p>A case is a prompt with mock tools and an expectation, or a recorded session.</p>
      </div>
    );
  }
  return (
    <div className="[ score-list ]" role="table" aria-label="Bench cases">
      {vm.cases.map((c) => (
        <div key={c.id} className="[ sc-row ]" data-kind="case" role="row" data-case={c.id}>
          <span className="[ title ]">{c.title}</span>
          <span className="[ meta ]">{c.source}</span>
          <span className="[ meta ]">{c.tools}</span>
        </div>
      ))}
    </div>
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
