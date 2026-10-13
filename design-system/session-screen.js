// Session screen specimen: builds the session page (head, tab row with its
// tools, terminal) in any state from plain options, with the markup the web UI
// renders and nothing but the design system's own blocks. The xterm output is
// faked (.xterm[data-fake]). Used by specimen.html and design/ mockups:
// declare frames as <div data-frame='{"w":1280,"h":760,…}'> and call
// Kit.mount(); each becomes an iframe at a real viewport size, scaled to fit,
// so the 64rem and 48rem rules behave as in the app. Styles for the frames and
// the fake output: session-screen.css.
const Kit = (() => {
  const ICONS = {
    more: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="currentColor"><circle cx="3.5" cy="8" r="1.3"/><circle cx="8" cy="8" r="1.3"/><circle cx="12.5" cy="8" r="1.3"/></svg>',
    jump: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 2.5v8M4.5 7 8 10.5 11.5 7M3.5 13.5h9"/></svg>',
    text: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1.5 12.5 5 3.5l3.5 9M2.8 9.5h4.4M10 12.5l2.25-5.5 2.25 5.5M10.7 11h3.1"/></svg>',
    focus: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 6V2.5H6M10 2.5h3.5V6M13.5 10v3.5H10M6 13.5H2.5V10"/></svg>',
    unfocus: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6 2.5V6H2.5M13.5 6H10V2.5M10 13.5V10h3.5M2.5 10H6v3.5"/></svg>',
    back: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10 3 5 8l5 5"/></svg>',
    copy: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M10.5 3.5v-.5a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h.5"/></svg>',
    trash: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 4.5h11M6.5 4.5V3h3v1.5M4 4.5l.7 8.6a1 1 0 0 0 1 .9h4.6a1 1 0 0 0 1-.9l.7-8.6"/></svg>',
    branch: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><circle cx="4.5" cy="3.5" r="1.5"/><circle cx="4.5" cy="12.5" r="1.5"/><circle cx="11.5" cy="5.5" r="1.5"/><path d="M4.5 5v6M11.5 7c0 3-7 2-7 4"/></svg>',
    stop: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="currentColor"><rect x="4" y="4" width="8" height="8" rx="1.5"/></svg>',
    reconnect: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M13 8a5 5 0 1 1-1.5-3.5M13 2.5v3h-3"/></svg>',
  };
  const icon = (n) => ICONS[n];

  // The agent's screen, from the person's screenshot. Newest row last.
  const WIDE = [
    '<span class="t-dim">  Read <span class="t-b">4</span> files (ctrl+o to expand)</span>',
    ' ',
    '● The PR adds three migrations that guard each FK rename with an information_schema',
    '  lookup. MySQL 8 can do this statically, so the guards only hide drift.',
    ' ',
    '<span class="t-dim">  Ran <span class="t-b">3</span> shell commands</span>',
    ' ',
    '● Listing the FK names main\'s migrations produce, so the static rewrites name the',
    '  right constraints.',
    ' ',
    '<span class="t-dim">  Ran <span class="t-b">7</span> shell commands</span>',
    ' ',
    '● I\'m reading the PR\'s three migrations and the migrations already in the repo. Next I\'ll',
    '  start a throwaway MySQL 8 to compare the guarded versions against static ones.',
    ' ',
    '<span class="t-dim">  Ran <span class="t-b">6</span> shell commands</span>',
    ' ',
    '● Starting a throwaway MySQL 8 container to test the migrations.',
    ' ',
    '● Baseline schema is built from main\'s migrations. The current FK names are',
    '  <span class="t-link">messaging_conversations_ibfk_2</span> and <span class="t-link">messaging_envelopes_ibfk_3</span>. Next I\'ll write the',
    '  static rewrites.',
    ' ',
    '  Running S=/private/tmp/claude-501/-Users-renandonatoferna… <span class="t-dim">· 8s</span>',
    '<span class="t-dim">  ⎿ $ S=/private/tmp/claude-501/-Users-renandonatofernandesbatista--coding-pool-wor</span>',
    '<span class="t-dim">    ktrees-msb-server-815e5857a0320a78-agent-messages-history-3d7da05b/scratchpad</span>',
    '<span class="t-dim">    cat &gt; $S/run.sh &lt;&lt;\'EOF\' … (8s · 2 lines)</span>',
    '<span class="t-dim">    (ctrl+b to run in background)</span>',
    ' ',
    '<span class="t-busy">✻ Ionizing…</span> <span class="t-dim">(4m 23s · ↓ 20.1k tokens)</span>',
    ' ',
    '<span class="t-rule">' + '─'.repeat(160) + '</span>',
    '❯ CURSOR',
    '<span class="t-rule">' + '─'.repeat(160) + '</span>',
    '  <span class="t-mode">⏵⏵ auto mode on</span> <span class="t-dim">(shift+tab to cycle) · esc to interrupt · ← 5 agents</span>',
  ];
  const NARROW = [
    '<span class="t-dim">  Read <span class="t-b">4</span> files</span>',
    ' ',
    '● The PR guards each FK rename',
    '  with an information_schema',
    '  lookup. MySQL 8 can do this',
    '  statically, so the guards only',
    '  hide drift.',
    ' ',
    '<span class="t-dim">  Ran <span class="t-b">3</span> shell commands</span>',
    ' ',
    '● Listing the FK names main\'s',
    '  migrations produce.',
    ' ',
    '<span class="t-dim">  Ran <span class="t-b">6</span> shell commands</span>',
    ' ',
    '● Starting a throwaway MySQL 8',
    '  container to test the migrations.',
    ' ',
    '● Baseline schema is built from',
    '  main\'s migrations. The current FK',
    '  names are',
    '  <span class="t-link">messaging_conversations_ibfk_2</span>',
    '  and <span class="t-link">messaging_envelopes_ibfk_3</span>.',
    '  Next I\'ll write the static',
    '  rewrites.',
    ' ',
    '  Running S=/private/tmp/claude-5… <span class="t-dim">· 8s</span>',
    '<span class="t-dim">  ⎿ cat &gt; $S/run.sh &lt;&lt;\'EOF\' …</span>',
    '<span class="t-dim">    (ctrl+b to run in background)</span>',
    ' ',
    '<span class="t-busy">✻ Ionizing…</span> <span class="t-dim">(4m 23s · ↓ 20.1k)</span>',
    ' ',
    '<span class="t-rule">' + '─'.repeat(60) + '</span>',
    '❯ CURSOR',
    '<span class="t-rule">' + '─'.repeat(60) + '</span>',
    '  <span class="t-mode">⏵⏵ auto mode on</span> <span class="t-dim">· esc to interrupt</span>',
  ];
  // The agent asks the person (waiting).
  const ASK = [
    ' ',
    '● I\'ll run the migrations against the throwaway database.',
    ' ',
    '<span class="t-rule">' + '─'.repeat(160) + '</span>',
    ' <span class="t-b">Bash command</span>',
    ' ',
    '   docker exec mig-mysql mysql -uroot -proot msb &lt; mig/static/0042_fk_names.up.sql',
    '   <span class="t-dim">Apply the static FK rewrite to the scratch database</span>',
    ' ',
    ' <span class="t-ask">Do you want to proceed?</span>',
    ' <span class="t-ask">❯ 1. Yes</span>',
    '   2. Yes, and don\'t ask again for <span class="t-b">docker exec</span> commands in this project',
    '   3. No, and tell Claude what to do differently <span class="t-dim">(esc)</span>',
    ' ',
  ];
  const ENDED = [
    ' ',
    '● Both FK rewrites pass up and down against MySQL 8. I pushed the branch and asked the',
    '  architect for review.',
    ' ',
    '<span class="t-dim">  Total cost: $1.84 · Total duration (API): 6m 12s · Total duration (wall): 19m 40s</span>',
    ' ',
    '<span class="t-dim">Resume this session with:</span>',
    '<span class="t-dim">claude --resume 3d7da05b-35c4-4a93-8053-c7d10a96e849</span>',
    '<span class="t-dim">renan@harness msb-server % </span>',
  ];
  const APP = [
    '<span class="t-dim">$ make dev</span>',
    'go run ./cmd/server --config dev.yaml',
    '<span class="t-dim">2026-10-09T15:02:11-03:00</span> <span class="t-ok">INF</span> migrations applied <span class="t-dim">count=42</span>',
    '<span class="t-dim">2026-10-09T15:02:11-03:00</span> <span class="t-ok">INF</span> listening <span class="t-dim">addr=:8080</span>',
    '<span class="t-dim">2026-10-09T15:02:19-03:00</span> <span class="t-ok">INF</span> GET /api/conversations <span class="t-dim">status=200 dur=4ms</span>',
    '<span class="t-dim">2026-10-09T15:02:20-03:00</span> <span class="t-mode">WRN</span> slow query <span class="t-dim">dur=312ms table=messaging_envelopes</span>',
    '<span class="t-dim">2026-10-09T15:02:24-03:00</span> <span class="t-ok">INF</span> GET /api/conversations/88 <span class="t-dim">status=200 dur=6ms</span>',
    'CURSOR',
  ];
  const SCREENS = { wide: WIDE, narrow: NARROW, ask: ASK, ended: ENDED, app: APP };
  const FONT = { sm: "12px", md: "13px", lg: "15px" };

  const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");
  const attr = (on, a) => (on ? " " + a : "");

  // --- session head -------------------------------------------------------
  const STATES = { running: "running", waiting: "waiting", done: "ended", failed: "failed", idle: "idle" };

  function checkButton(o) {
    if (!o.checks) return "";
    const c = o.checks;
    const dot = { on: "live", paused: "idle", error: "failed", busy: "live" }[c];
    const next = { on: "next 15m", paused: "paused", error: "couldn't save", busy: "saving…" }[c];
    return `<button class="button check-button" data-variant="ghost" data-size="sm" aria-haspopup="dialog" aria-expanded="${!!o.checksOpen}"${attr(c === "busy", 'aria-busy="true"')} aria-label="Status checks: every 20 minutes, ${next}">
      <span class="status" data-state="${dot}">Checks · 20m</span><span class="next">${next}</span><span class="chevron"></span></button>`;
  }

  function checkPanel(o) {
    const c = o.checks;
    const paused = c === "paused";
    const stateWord = paused ? '<span class="status" data-state="idle">Paused</span><span>since 15:04</span>' : '<span class="status" data-state="live">On</span><span>next in 15m</span><span>· no check yet</span>';
    const err = c === "error" ? `<div class="message" data-tone="danger" role="alert">Couldn't change the interval. <button class="button" data-size="sm" data-variant="ghost">Retry</button></div>` : "";
    return `<div class="popup" data-static role="dialog" aria-labelledby="cp-${o.uid}" style="${o.popAt || "inset-block-start: calc(100% - 0.35rem); inset-inline-end: 7.5rem"}">
      <div class="check-panel">
        <h3 id="cp-${o.uid}">Status checks</h3>
        <p class="state">${stateWord}</p>
        <div class="field-row"><label for="ci-${o.uid}">Check every</label>
          <select id="ci-${o.uid}" data-size="sm" data-fit${attr(c === "busy", "disabled aria-busy=\"true\"")}>
            <option>2 minutes</option><option>5 minutes</option><option>10 minutes</option><option selected>20 minutes</option><option>30 minutes</option><option>1 hour</option>
          </select></div>
        <p class="hint">The task's architect is woken on this timer to check on this session. Changing it takes effect after the next check.</p>
      </div>${err}
      <footer><span>Last check: none yet</span><button class="button" data-size="sm"${attr(c === "busy", "disabled")}>${paused ? "Resume checks" : "Pause checks"}</button></footer>
    </div>`;
  }

  function moreMenu(o) {
    const pop = o.menuOpen ? `<div class="popup" data-static role="menu" aria-label="Session actions" style="inset-block-start: calc(100% + 0.25rem); inset-inline-end: 0">
        <button role="menuitem" data-narrow>${icon("branch")}Git history</button>
        <button role="menuitem">${icon("copy")}Copy session id <span class="meta">3d7da05b</span></button>
        <hr role="separator">
        <button role="menuitem" data-tone="danger">${icon("trash")}Delete session…</button>
      </div>` : "";
    const confirm = o.confirmDelete ? `<div class="popup" data-static role="alertdialog" aria-labelledby="cd-${o.uid}" aria-describedby="cdd-${o.uid}" style="inset-block-start: calc(100% + 0.25rem); inset-inline-end: 0">
        <div class="confirm-panel"><h3 id="cd-${o.uid}">Delete this session?</h3>
        <p id="cdd-${o.uid}">${o.state === "done" || o.state === "failed" ? "Its record and worktree are removed; its branch and commits stay." : "It is still running: it stops now. Its worktree is removed; its branch and commits stay."}</p></div>
        <footer><button class="button" data-variant="ghost" data-size="sm">Keep it</button><button class="button" data-variant="danger" data-size="sm"${attr(o.deleting, 'aria-busy="true"')}>Delete session</button></footer>
      </div>` : "";
    return `<div class="menu" style="position: relative"><button class="button" data-variant="ghost" data-size="sm" data-icon aria-label="More session actions" aria-haspopup="menu" aria-expanded="${!!(o.menuOpen || o.confirmDelete)}">${icon("more")}</button>${pop}${confirm}</div>`;
  }

  function head(o) {
    const st = o.state || "running";
    const word = o.word || STATES[st];
    const running = st === "running" || st === "waiting";
    const pr = o.pr ? `<a class="badge" href="#" data-tone="${o.pr === "merged" ? "signal" : "neutral"}" title="Pull request #128 · ${o.pr}">PR #128 · ${o.pr}</a>` : "";
    return `<header class="session-head" style="position: relative">
      ${o.back === false ? "" : `<button class="button" data-variant="ghost" data-size="sm" data-icon data-split-back aria-label="Sessions">${icon("back")}</button>`}
      <h2>${esc(o.title || "Messages history")}</h2>
      <p class="meta"><span class="status" data-state="${st}"${attr(o.signal, "data-signal")}>${word}</span><span class="who">${esc(o.who || "database-manager · 4m")}</span>${o.checks ? `<span class="checks">· checks ${o.checks === "paused" ? "paused" : "20m"}</span>` : ""}</p>
      <div class="actions">
        ${pr}
        ${checkButton(o)}
        <a class="button" data-variant="ghost" data-size="sm" href="#" data-wide>Git history</a>
        ${running ? `<button class="button" data-size="sm"${attr(o.stopping, 'aria-busy="true"')}>Stop</button>` : ""}
        ${moreMenu(o)}
      </div>
      ${o.checksOpen ? checkPanel(o) : ""}
    </header>`;
  }

  // --- pane tabs ------------------------------------------------------------
  function paneTabs(o) {
    const tab = o.tab || "agent";
    const terminalTab = tab === "agent" || tab === "app";
    const conn = o.conn || "live";
    let tools = "";
    if (terminalTab) {
      const textBtn = `<div class="menu pref" style="position: relative"><button class="button" data-variant="ghost" data-size="sm" data-icon aria-label="Text size" title="Text size" aria-haspopup="menu" aria-expanded="${!!o.textMenu}">${icon("text")}</button>${o.textMenu ? `
        <div class="popup" data-static role="menu" aria-label="Text size" style="inset-block-start: calc(100% + 0.25rem); inset-inline-end: 0; min-inline-size: 11rem">
          <div class="group">Text size</div>
          ${[["sm", "Smaller", "12px"], ["md", "Default", "13px"], ["lg", "Larger", "15px"]].map(([k, l, px]) => `<button role="menuitemradio" aria-checked="${(o.text || "md") === k}">${l}<span class="meta">${px}</span></button>`).join("")}
        </div>` : ""}</div>`;
      const focusBtn = o.focus
        ? `<button class="button exit-focus pref" data-variant="ghost" data-size="sm" aria-pressed="true">${icon("unfocus")}<span class="word">Exit focus</span></button>`
        : `<button class="button pref" data-variant="ghost" data-size="sm" data-icon aria-pressed="false" aria-label="Focus mode" title="Focus mode">${icon("focus")}</button>`;
      const reconnect = `<button class="button" data-size="sm"${attr(o.reconnecting, 'aria-busy="true"')}>${icon("reconnect")}<span class="word">Reconnect</span></button>`;
      if (conn === "offline") {
        tools = `<span class="status" data-state="offline" role="status"><span class="word">offline · lost 12s ago</span></span>${reconnect}`;
      } else if (conn === "reconnecting") {
        tools = `<span class="status" data-state="reconnecting" role="status"><span class="word">reconnecting · try 2 of 5</span></span>${reconnect}`;
      } else if (conn === "connecting") {
        tools = `<span class="status" data-state="queued" role="status"><span class="word">connecting…</span></span>${reconnect.replace("<button", "<button disabled")}`;
      } else {
        // Live, or ended (the pane's note says so): a quiet dot, words in its tooltip.
        const ended = conn === "ended";
        const tip = ended ? (tab === "app" ? "Exited with code 1" : "Ended · exit 0") : (tab === "app" ? "Live · make dev" : "Live · ✳ Ionizing… (window title)");
        const dot = `<span class="status" data-state="${ended ? "done" : "live"}" data-dot-only role="status" aria-label="${tip}" title="${tip}"></span>`;
        const jump = o.jump ? `<button class="button jump" data-size="sm" aria-label="Jump to latest, ${o.jump} new lines">${icon("jump")}<span>${o.jump > 999 ? "999+" : o.jump}</span><span class="word">new lines</span></button>` : "";
        tools = `${dot}${jump}<span class="sep"></span>${textBtn}${focusBtn}`;
      }
    }
    const lead = o.focus ? `<span class="lead"><span class="status" data-state="${o.state || "running"}" data-dot-only aria-label="${o.state || "running"}"></span><span>${esc(o.title || "Messages history")}</span></span>` : "";
    const t = (id, name, extra = "") => `<button role="tab" aria-selected="${tab === id}"${attr(tab !== id, 'tabindex="-1"')}>${name}${extra}</button>`;
    const attention = o.attention ? ' <span class="status" data-state="waiting" data-dot-only aria-label="waiting on you"></span>' : "";
    return `<div class="pane-tabs"${attr(o.focus, "data-focus")}>${lead}
      <div class="tabs" role="tablist" aria-label="Session views">${t("agent", "Agent", attention)}${t("history", "History")}${t("app", "App")}${t("design", "Design", o.designBadge === false ? "" : ' <span class="badge" data-tone="signal">2</span>')}${t("conversation", "Conversation")}</div>
      ${tools ? `<div class="tools" role="group" aria-label="${tab === "app" ? "Run terminal tools" : "Terminal tools"}">${tools}</div>` : ""}
    </div>`;
  }

  // --- terminal -------------------------------------------------------------
  function terminal(o) {
    const st = o.conn || "live";
    const screen = o.screen || "wide";
    let rows = SCREENS[screen].slice();
    if (o.scrolled) rows = rows.slice(0, -8);
    const cursorCls = o.focused ? "" : " data-blur";
    const body = st === "connecting" ? "" : rows.map((r) => `<div class="l">${r.replace("CURSOR", '<span class="xterm-cursor"></span>')}</div>`).join("");
    const notice = st === "connecting" ? `<p class="notice" role="status"><span class="skeleton" style="inline-size: 9rem; block-size: .5rem"></span>Connecting to the terminal…</p>` : "";
    const hist = o.history === false ? "" : " data-history";
    const thumb = o.scrolled ? ' style="--_thumb-top: 38%; --_thumb-bottom: 22%"' : "";
    const failed = o.state === "failed";
    const note = st === "ended" ? `<div class="pane-note"${attr(failed, 'data-tone="danger"')} role="status"><span class="text"><strong>${failed ? "This session failed." : "This session ended."}</strong> ${failed ? "claude exited with code 1 at 15:41." : "claude exited at 15:41. The screen below is its last, read-only."}</span><button class="button" data-variant="primary" data-size="sm"${attr(o.resuming, 'aria-busy="true"')}>Resume session</button></div>` : "";
    return `<div class="terminal" data-state="${st}"${attr(o.attention, "data-attention")}>${note}
      <div class="screen"${hist}${cursorCls}${thumb}${attr(o.focused, 'tabindex="-1"')}><div class="host"><div class="xterm" data-fake style="--_fs: ${FONT[o.text || "md"]}"${attr(o.scrolled, "data-scrolled")}>${body}</div></div></div>
      ${notice}
    </div>`;
  }

  // --- history: the saved conversation, read-only ---------------------------
  const HISTORY = [
    ["user", "›", "14:02", "Why does the agent tab stop scrolling?"],
    ["agent", "⏺", "14:02", "claude draws on the alternate screen, so the terminal keeps no history. The full conversation is here."],
    ["tool", "⌁", "14:02", null, "Grep", "onData|wheel in web/src"],
    ["tool", "⌁", "14:03", null, "Bash", "git status --short", "3 lines"],
    ["error", "✕", "14:03", null, "Bash", "exit 1 · go test ./internal/...", "48 lines"],
    ["agent", "⏺", "14:04", "Fixed: the wheel now pages claude's own view, and History reads the transcript."],
  ];
  function history(o) {
    const lines = HISTORY.map(([kind, mark, at, text, tool, gist, meta]) => text !== null
      ? `<p class="line" data-kind="${kind}"><time>${at}</time><span class="mark" aria-hidden="true">${mark}</span><span class="text">${esc(text)}</span></p>`
      : `<div class="line" data-kind="${kind}"><time>${at}</time><span class="mark" aria-hidden="true">${mark}</span><div class="fold"><button class="summary" aria-expanded="false"><span class="tool">${tool}</span><span class="gist">${esc(gist)}</span>${meta ? `<span class="meta">${meta}</span>` : ""}</button><pre class="detail"></pre></div></div>`).join("");
    const jump = o.jump ? `<button class="button jump" data-size="sm">${o.jump} new · Jump to latest</button>` : "";
    return `<section class="frame" aria-label="History"><div class="transcript" data-scroll role="log" tabindex="0" aria-label="Conversation history"><div class="older"><button class="button" data-variant="ghost" data-size="sm">Load older</button></div>${lines}${jump}</div></section>`;
  }

  // --- a whole session column ----------------------------------------------
  function session(o) {
    const tab = o.tab || "agent";
    const pane = tab === "history" ? history(o) : tab === "design" || tab === "conversation"
      ? `<div class="empty" style="display:grid;place-content:center;color:var(--color-ink-faint);font-size:var(--text-sm)">${tab === "design" ? "Design tab: no tools in the tab row" : "Conversation tab: no tools in the tab row"}</div>`
      : tab === "app"
        ? `<div class="app-pane"><div class="app-cmd"><span class="status" data-state="running">running</span><code>make dev</code><span class="grow"></span><button class="button" data-size="sm">Stop</button></div>${terminal({ ...o, screen: "app" })}</div>`
        : terminal({ ...o, screen: o.screen || (o.attention ? "ask" : o.conn === "ended" ? "ended" : o.phone ? "narrow" : "wide") });
    return `<section class="session"${attr(o.focus, "data-focus")} aria-label="Session">${o.noHead ? "" : head(o)}${o.noTabs ? "" : paneTabs(o)}${o.noPane ? "" : pane}</section>`;
  }

  const LIST = `<div class="list-stub" aria-label="Sessions">
    <div class="row"><span class="status" data-state="running">running</span><span>Architect · Fix agent terminal…</span><span class="meta">architect · 2h</span></div>
    <div class="row" aria-selected="true"><span class="status" data-state="running">running</span><span>Messages history</span><span class="meta">database-manager · 4m</span></div>
    <div class="row"><span class="status" data-state="waiting">waiting</span><span>Terminal clipping fix</span><span class="meta">frontend-developer · 12m</span></div>
    <div class="row"><span class="status" data-state="done">done</span><span>Dropdown component</span><span class="meta">ux-designer · 1d</span></div></div>`;

  const RAIL = `<div class="rail"><a class="button" href="#">New task</a>
    <div class="stub"><span class="h">Project</span><span>Board</span><span>Review requests</span><span>Documents</span><span>Design assets</span>
    <span class="h">In progress · 3</span><span aria-current="page">Fix agent terminal layout</span><span>Messages history</span><span>Task rail seed</span>
    <span class="h">Review · 1</span><span>Dropdown variants</span></div></div>`;

  // The phone's ⋯ sheet.
  function sheet(o) {
    return `<div class="scrim"></div>
    <div class="popup" data-static data-sheet role="dialog" aria-labelledby="sh-${o.uid}">
      <header><span id="sh-${o.uid}">Messages history</span><button class="button" data-variant="ghost">Close</button></header>
      <div class="sheet-checks"><span class="status" data-state="live">Checks · next 15m</span>
        <label class="visually-hidden" for="sc-${o.uid}">Check every</label><select id="sc-${o.uid}" data-fit><option>2 min</option><option>10 min</option><option selected>20 min</option><option>1 hour</option></select></div>
      <div class="menu" role="menu" style="display: block">
        <button role="menuitem">Pause checks</button>
        <hr role="separator">
        <button role="menuitem">${icon("branch")}Git history</button>
        <button role="menuitem">${icon("text")}Text size <span class="meta">Default</span></button>
        <button role="menuitem">${icon("copy")}Copy session id</button>
        <button role="menuitem">${icon("stop")}Stop session</button>
        <hr role="separator">
        <button role="menuitem" data-tone="danger">${icon("trash")}Delete session…</button>
      </div>
    </div>`;
  }
  function sheetConfirm(o) {
    return `<div class="scrim"></div>
    <div class="popup" data-static data-sheet role="alertdialog" aria-labelledby="scd-${o.uid}">
      <div class="confirm-panel" style="inline-size: auto; padding: var(--space-l)"><h3 id="scd-${o.uid}">Delete this session?</h3><p>It is still running: it stops now. Its worktree is removed; its branch and commits stay.</p></div>
      <footer style="padding: var(--space-s) var(--space-l) var(--space-l)"><button class="button" data-variant="ghost" style="min-block-size: var(--size-touch)">Keep it</button><button class="button" data-variant="danger" style="min-block-size: var(--size-touch)">Delete session</button></footer>
    </div>`;
  }

  // A whole page: shell, task toolbar, list beside the session, stream bar.
  function shell(o) {
    return `<div class="app-shell"${attr(o.focus, "data-focus")}>
      <header class="topbar"><button class="button tasks-toggle" data-variant="ghost">☰ Tasks</button><a class="brand" href="#">harness</a><select aria-label="Project" data-size="sm"><option>harness</option></select></header>
      ${RAIL}
      <main id="main">
        <header class="toolbar"><a class="crumb" href="#">harness /</a><h1>Fix agent terminal layout</h1><div class="actions"><a class="button" data-variant="ghost" data-size="sm">Documents <span class="badge">8</span></a><a class="button" data-variant="ghost" data-size="sm">Design assets <span class="badge">5</span></a><button class="button" data-variant="primary" data-size="sm">New session</button></div></header>
        <div class="split-view"${attr(o.detail !== false, "data-detail")}>${LIST}${session(o)}</div>
      </main>
      <footer class="stream-bar" aria-live="polite"><span class="status" data-state="live">live · :8080</span><span>4 sessions · 1 waiting</span><span class="keys"><span><kbd>⌘K</kbd> commands</span><span><kbd>J</kbd><kbd>K</kbd> move</span></span></footer>
    </div>${o.sheet ? sheet(o) : ""}${o.sheetConfirm ? sheetConfirm(o) : ""}`;
  }

  // One component on its own, inside a session column of a given width.
  function comp(o) {
    const parts = o.parts || ["head", "tabs"];
    // Outside a split view nothing hides the narrow-mode back button: leave it out.
    const inner = parts.map((p) => (p === "head" ? head({ ...o, back: false }) : p === "tabs" ? paneTabs(o) : p === "history" ? history(o) : terminal({ ...o, screen: o.screen || "wide" }))).join("");
    return `<div class="comp"><section class="session" style="inline-size: ${o.col ? o.col + "px" : "auto"}"${attr(o.focus, "data-focus")}>${inner}</section></div>`;
  }

  // xterm shows whole rows only: drop rows off the top until the rest fit.
  function fitRows(root = document) {
    root.querySelectorAll(".xterm[data-fake]").forEach((el) => {
      const cs = getComputedStyle(el);
      const room = el.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom);
      if (room <= 0 || !el.firstElementChild) return;
      const used = () => el.lastElementChild.getBoundingClientRect().bottom - el.firstElementChild.getBoundingClientRect().top;
      while (el.children.length > 1 && used() > room + 0.5) el.firstElementChild.remove();
    });
  }

  // Frames: each is an iframe at a real viewport size, so the 64rem and
  // 48rem media rules behave as they will in the app; scaled to its column.
  function mount() {
    const styles = [...document.querySelectorAll('style, link[rel="stylesheet"]')]
      .filter((el) => el.tagName === "STYLE" || !/fonts\.googleapis/.test(el.href))
      .map((el) => (el.tagName === "STYLE" ? el.outerHTML : `<link rel="stylesheet" href="${el.href}">`)).join("\n");
    const kitEl = document.querySelector("script[data-kit]");
    const kit = kitEl.src ? `<script src="${kitEl.src}"><\/script>` : `<script>${kitEl.textContent}<\/script>`;
    let uid = 0;
    document.querySelectorAll("[data-frame]").forEach((el) => {
      const o = JSON.parse(el.dataset.frame);
      o.uid = ++uid;
      const body = o.what === "comp" ? comp(o) : shell(o);
      const theme = el.closest("[data-theme]")?.dataset.theme || o.theme || "light";
      const doc = `<!doctype html><html lang="en" data-theme="${theme}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">${styles}</head>
        <body class="in-frame">${body}${kit}<script>addEventListener("load", () => Kit.fitRows());<\/script></body></html>`;
      const frame = document.createElement("iframe");
      frame.setAttribute("sandbox", "allow-scripts");
      frame.setAttribute("title", el.getAttribute("aria-label") || "Mockup frame");
      frame.width = o.w; frame.height = o.h;
      frame.srcdoc = doc;
      el.classList.add("mock");
      if (o.device) el.dataset.device = o.device;
      el.append(frame);
      const fit = () => {
        const room = (o.device === "phone" ? o.w : el.parentElement.clientWidth) - 2;
        const k = Math.min(1, room / o.w);
        frame.style.transform = `scale(${k})`;
        el.style.inlineSize = o.device === "phone" ? "" : Math.round(o.w * k + 2) + "px";
        el.style.blockSize = Math.round(o.h * k + (o.device === "phone" ? 12 : 2)) + "px";
      };
      fit();
      new ResizeObserver(fit).observe(el.parentElement);
    });
  }

  return { head, paneTabs, terminal, session, comp, shell, mount, fitRows, icon };
})();
