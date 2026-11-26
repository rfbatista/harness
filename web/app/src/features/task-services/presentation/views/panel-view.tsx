// The painel de serviços da task: a docked column beside the whiteboard, an
// overlay on its edge, or a bottom sheet (UX study "Design: painel de serviços
// na tela da task"). It renders what the presenter gives it.

import { useEffect, useRef } from "react";
import { Icon } from "../../../../shared/presentation/icon";
import { usePresenter } from "../../../../shared/presentation/use-presenter";
import type { LogPresenter } from "../presenters/log-presenter";
import type { PanelPresenter, RequestVM, RowVM, SeloVM } from "../presenters/panel-presenter";

const DOT = { created: "idle", running: "running", starting: "reconnecting", stopped: "idle", failed: "failed", pending: "reconnecting" } as const;

function Selo({ selo }: { selo: SeloVM }) {
  if (selo.value === 0) return null;
  return (
    <span className="badge" data-selo data-tone={selo.tone} title={selo.label} aria-label={selo.label}>
      {selo.value}
    </span>
  );
}

function Credentials({ row, presenter }: { row: RowVM; presenter: PanelPresenter }) {
  if (row.credentialsCount === 0) return null;
  return (
    <div className="task-service-credentials">
      <button type="button" className="button" data-variant="ghost" data-size="sm" aria-expanded={row.credentialsOpen} onClick={() => presenter.toggleCredentials(row.id)}>
        Credentials · {row.credentialsCount}
      </button>
      {row.credentialsOpen && (
        <ul className="[ credential-box ]">
          {row.credentials.map((c, i) => (
            <li key={i}>
              <span className="label">{c.label}</span>
              <span className="value" aria-label={c.shown ? undefined : "value hidden"}>{c.text}</span>
              <button type="button" className="button" data-variant="ghost" data-size="sm" onClick={() => presenter.toggleShown(row.id, i)}>{c.shown ? "Hide" : "Show"}</button>
              <button type="button" className="button" data-variant="ghost" data-size="sm" onClick={() => void presenter.copy(row.id, i)}>{c.copied ? "Copied" : "Copy"}</button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Row({ row, presenter }: { row: RowVM; presenter: PanelPresenter }) {
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (row.confirm) cancel.current?.focus();
  }, [row.confirm]);
  return (
    <li className="task-service" data-service={row.id} data-group={row.group} data-ring={row.ringed ? "" : undefined}>
      <div className="head">
        <span className="name">{row.name}</span>
        <span className="meta">{row.time}</span>
      </div>
      <div className="status-line">
        <span className="status" data-state={DOT[row.dot]}>{row.word}</span>
        {row.what !== "" && <span className="what">{row.what}</span>}
      </div>
      {row.error !== "" && <p className="error-line">{row.error}</p>}
      {row.ports.length > 0 && (
        <p className="ports">
          {row.ports.map((p) => (p.href === "" ? <span key={p.text}>{p.text}</span> : <a key={p.text} href={p.href} target="_blank" rel="noreferrer" aria-label={p.name}>{p.text}</a>))}
        </p>
      )}
      {row.requestBadge > 0 && <span className="badge" data-tone="attention">{row.requestBadge} request{row.requestBadge === 1 ? "" : "s"}</span>}
      <Credentials row={row} presenter={presenter} />
      {row.refusal && (
        <div className="confirm-strip" data-tone="danger" role="alert">
          <h3>{row.refusal.message}</h3>
          <p className="code">{row.refusal.code}</p>
          <div className="actions"><button type="button" className="button" onClick={() => presenter.dismissRefusal(row.id)}>Try again</button></div>
        </div>
      )}
      {row.confirm ? (
        <div className="confirm-strip" data-tone={row.confirm === "stop" ? "danger" : undefined}>
          <h3>{row.confirmText}</h3>
          <div className="actions">
            <button type="button" className="button" data-variant={row.confirm === "stop" ? "danger" : "primary"} onClick={() => presenter.confirm(row.id)}>{row.confirm === "stop" ? "Stop" : "Restart"}</button>
            <button type="button" className="button" ref={cancel} onClick={() => presenter.cancel(row.id)}>Cancel</button>
          </div>
        </div>
      ) : (
        <div className="actions">
          <button type="button" className="button" data-variant="ghost" data-size="sm" onClick={() => presenter.openLog(row.id)}>{row.group === "attention" ? "Open log" : "Log"}</button>
          <span className="spacer" />
          {row.canStart && <button type="button" className="button" data-size="sm" disabled={row.busy} data-busy={row.busy ? "" : undefined} onClick={() => presenter.request(row.id, "start")}>Start</button>}
          {row.canRestart && <button type="button" className="button" data-size="sm" disabled={row.busy} data-busy={row.busy ? "" : undefined} onClick={() => presenter.request(row.id, "restart")}>Restart</button>}
          {row.canStop && <button type="button" className="button" data-size="sm" disabled={row.busy} onClick={() => presenter.request(row.id, "stop")}>Stop</button>}
        </div>
      )}
    </li>
  );
}

function Request({ request, presenter }: { request: RequestVM; presenter: PanelPresenter }) {
  if (request.outcome !== "") return <li className="task-request" data-outcome><p className="meta">{request.outcome}</p></li>;
  return (
    <li className="task-request">
      <p className="title"><strong>{request.action}</strong> {request.serviceName}</p>
      <p className="meta">{request.agent}</p>
      {request.summary !== "" && <p>{request.summary}</p>}
      {request.reason !== "" && <p className="reason">{request.reason}</p>}
      {request.refusal && <p className="error-line" role="alert">{request.refusal.message} <span className="code">{request.refusal.code}</span></p>}
      <div className="actions">
        <button type="button" className="button" data-variant="primary" data-size="sm" disabled={request.busy} onClick={() => void presenter.answer(request.id, "approve")}>Approve</button>
        <button type="button" className="button" data-size="sm" disabled={request.busy} onClick={() => void presenter.answer(request.id, "deny")}>Deny</button>
        <a className="button" data-variant="ghost" data-size="sm" href="/services">Details ↗</a>
      </div>
    </li>
  );
}

function LogView({ log, presenter }: { log: LogPresenter; presenter: PanelPresenter }) {
  const vm = usePresenter(log);
  const view = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = view.current;
    if (el && vm.following) el.scrollTop = el.scrollHeight;
  }, [vm.lines, vm.following]);
  return (
    <div className="task-services-log">
      <div className="head">
        <button type="button" className="button" data-variant="ghost" data-size="sm" onClick={() => presenter.closeLog()}>‹ Services</button>
        <span className="status" data-state={vm.live ? "live" : "offline"}>{vm.following ? "Following" : "Paused · scrolled up"}</span>
      </div>
      <div className="lines" role="log" ref={view} onScroll={(e) => { const t = e.currentTarget; log.scrolled(t.scrollHeight - t.scrollTop - t.clientHeight < 8); }}>
        {vm.loading && <p className="meta">Loading the log…</p>}
        {vm.error !== "" && <p className="error-line" role="alert">{vm.error}</p>}
        {vm.empty && <p className="meta">Nothing in the log yet.</p>}
        {vm.lines.map((l, i) => <div key={i} className="line" data-tone={l.tone === "plain" ? undefined : l.tone}>{l.text}</div>)}
      </div>
      {!vm.following && <button type="button" className="button" data-size="sm" onClick={() => log.resume()}>Follow</button>}
      <div className="foot">
        <button type="button" className="button" data-size="sm" onClick={() => { presenter.request(log.serviceId, "restart"); presenter.closeLog(); }}>Restart</button>
        <a href="/services">Open in Services ↗</a>
      </div>
    </div>
  );
}

export function PanelView({ presenter }: { presenter: PanelPresenter }) {
  const vm = usePresenter(presenter);
  const root = useRef<HTMLElement>(null);
  const log = presenter.log;
  useEffect(() => {
    if (vm.focusId === "") return;
    root.current?.querySelector(`[data-service="${CSS.escape(vm.focusId)}"]`)?.scrollIntoView({ block: "nearest" });
    presenter.focused();
  }, [vm.focusId, presenter]);
  const handle = (
    <button type="button" className="task-services-handle" aria-expanded={vm.open} title="Services (S)" data-signal={vm.signal > 0 ? vm.signal : undefined} onClick={() => presenter.toggle()}>
      <Selo selo={vm.selo} />
      <span className="dots" aria-hidden="true">{vm.dots.map((d, i) => <span key={i} className="status" data-state={DOT[d]} />)}</span>
      <span className="label">Services{vm.mode === "sheet" && vm.selo.value > 0 ? ` ${vm.selo.value}` : ""}</span>
    </button>
  );
  const toast = vm.toast && (
    <div className="toast-region">
      <div
        className="toast"
        data-tone="danger"
        role="alert"
        onMouseEnter={() => presenter.toastEngaged(true)}
        onMouseLeave={() => presenter.toastEngaged(false)}
        onFocus={() => presenter.toastEngaged(true)}
        onBlur={() => presenter.toastEngaged(false)}
        onKeyDown={(e) => e.key === "Escape" && presenter.dismissToast()}
      >
        <span className="status" data-state="failed" />
        <div className="body">
          <strong>{vm.toast.text}</strong>
          <p className="meta">{vm.toast.context}</p>
          <div className="actions">
            {vm.toast.count === 1 && <button type="button" className="button" data-size="sm" onClick={() => presenter.openLogFromToast()}>Open log</button>}
            <button type="button" className="button" data-size="sm" onClick={() => presenter.showInPanel()}>Show in panel</button>
          </div>
        </div>
        <button type="button" className="button" data-variant="ghost" data-size="sm" aria-label="Dismiss" onClick={() => presenter.dismissToast()}>×</button>
      </div>
    </div>
  );
  return (
    <aside
      className="task-services"
      ref={root}
      aria-label="Task services"
      data-open={vm.open ? "" : undefined}
      data-mode={vm.mode}
      data-wide={vm.logOpen ? "" : undefined}
      onKeyDown={(e) => presenter.key(e.key) && e.stopPropagation()}
    >
      {!vm.open ? handle : (
        <section className="panel">
          <div className="head">
            <Icon name="server" />
            <h2>Services</h2>
            <Selo selo={vm.selo} />
            <span className="status" data-state={vm.live ? "live" : "offline"}>{vm.live ? "live" : "paused"}</span>
            <button type="button" className="button" data-variant="ghost" data-size="sm" title="Collapse (S)" aria-label="Collapse the services panel" onClick={() => presenter.collapse()}>⟩</button>
          </div>
          {log ? <LogView log={log} presenter={presenter} /> : (
            <div className="body">
              {vm.error !== "" && <div className="confirm-strip" data-tone="danger" role="alert"><h3>{vm.error}</h3><div className="actions"><button type="button" className="button" onClick={() => presenter.retry()}>Try again</button></div></div>}
              {vm.loading && <p className="meta">Loading services…</p>}
              {vm.requests.length > 0 && (
                <section className="band" aria-label="Waiting on you">
                  <h3>Waiting on you</h3>
                  <ul>{vm.requests.map((r) => <Request key={r.id} request={r} presenter={presenter} />)}</ul>
                </section>
              )}
              {vm.empty && (
                <div className="empty-state">
                  <p><strong>No services yet</strong></p>
                  <p className="meta">Services this task’s agents create or run show up here.</p>
                  <p className="meta">Their requests to you show up here too.</p>
                </div>
              )}
              {vm.groups.map((g) => (
                <section key={g.key} data-group={g.key}>
                  <h3>{g.label}</h3>
                  <ul>{g.rows.map((r) => <Row key={r.id} row={r} presenter={presenter} />)}</ul>
                </section>
              ))}
            </div>
          )}
          <div className="foot"><span>Only this task’s services</span><a href="/services">All project services ↗</a></div>
        </section>
      )}
      <p className="visually-hidden" role="status" aria-live="polite">{vm.announcement}</p>
      {toast}
    </aside>
  );
}
