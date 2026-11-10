// What the MCP server form needs (UX spec section 5): the form's starting
// point (empty, a copy, or the server being edited), the checks the page can
// make at once, token detection with the variable a secret moves to, the
// splitting of a pasted command line, who the server reaches, where a refusal
// lands, and how the connection test reads.
//
// A secret value is never in anything this module returns for the page to show:
// the warnings carry the kind of token and the variable's name, never the
// value, and the server's answers are hidden of kept values before the page
// lays them out. The page never builds file bytes: every preview is the
// server's dry run.

import { varNames } from "./mcp.js";
import { detectToken, HIDDEN } from "./tokens.js";
import { mcpNameProblem, placeholderProblem } from "./names.js";
import { previewModel } from "./skillForm.js";

/** @typedef {import("./ports.js").Catalog} Catalog */
/**
 * @typedef {object} ValueRow
 * @property {number} id  for the page's lists
 * @property {string} key  the variable or header name
 * @property {string} value
 * @property {"env"|"text"} mode  From env writes ${VAR}; Plain text writes the value
 * @property {boolean} kept  the person chose to write a token as plain text
 *
 * @typedef {object} McpDraft
 * @property {string} previousName  "" for a new server
 * @property {string} name
 * @property {"stdio"|"http"|"sse"} transport
 * @property {string} command
 * @property {{ id: number, value: string }[]} args
 * @property {ValueRow[]} env
 * @property {string} url
 * @property {ValueRow[]} headers
 * @property {string[]} agents  every agent that gets it after the save
 * @property {string[]} projects  every project id that gets it after the save
 * @property {string[] | null} knownTools  the tool names of the server's last successful probe; null: tools unknown
 * @property {string} probedAt  when knownTools was taken, "" while unknown
 * @property {string[]} disabledTools  the tool default: the names of the disabled tools, hidden ones included
 * @property {object} base  what the form started with
 */

const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

let nextId = 1;
const freshId = () => nextId++;

// Tokens (domain/tokens.js).

export { detectToken };

const snake = (s) => String(s).replace(/[^A-Za-z0-9]+/g, "_").replace(/^_+|_+$/g, "").toUpperCase();

/**
 * The variable a secret moves to, named from the header or, failing that, the
 * server: Authorization on "linear" is LINEAR_API_KEY.
 * @param {{ kind: "arg"|"env"|"header", key: string, server: string }} o
 */
export function referenceName({ kind, key, server }) {
  let owner = snake(server) || "MCP";
  if (/^[0-9]/.test(owner)) owner = `MCP_${owner}`;
  if (kind === "env") {
    const own = snake(key);
    return own !== "" && !/^[0-9]/.test(own) ? own : `${owner}_API_KEY`;
  }
  if (kind === "arg") return `${owner}_API_KEY`;
  const header = snake(key);
  if (header === "" || header === "AUTHORIZATION" || header === "PROXY_AUTHORIZATION") return `${owner}_API_KEY`;
  let base = header.replace(/^X_/, "");
  if (!/KEY|TOKEN|SECRET|PASSWORD/.test(base)) base += "_TOKEN";
  return base === owner || base.startsWith(`${owner}_`) ? base : `${owner}_${base}`;
}

/** The value with its secret replaced by ${name}; the scheme or flag in front stays. @param {string} value @param {string} name */
export function useReference(value, name) {
  const ref = "${" + name + "}";
  const found = detectToken(value);
  return found ? value.replace(found.secret, () => ref) : ref;
}

/** A bare variable name typed under From env becomes its ${reference}. @param {string} text */
export function toReference(text) {
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(text) ? "${" + text + "}" : text;
}

// A pasted command line.

/**
 * Splits a command line the way a shell would: spaces separate, quotes group,
 * a backslash escapes. An unterminated quote runs to the end.
 * @param {string} text
 * @returns {string[]}
 */
export function splitCommandLine(text) {
  const out = [];
  let cur = "";
  let started = false;
  let quote = "";
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      if (c === quote) quote = "";
      else if (c === "\\" && quote === '"' && (text[i + 1] === '"' || text[i + 1] === "\\")) cur += text[++i];
      else cur += c;
    } else if (c === '"' || c === "'") {
      quote = c;
      started = true;
    } else if (c === "\\" && i + 1 < text.length) {
      cur += text[++i];
      started = true;
    } else if (/\s/.test(c)) {
      if (started || cur !== "") out.push(cur);
      cur = "";
      started = false;
    } else {
      cur += c;
      started = true;
    }
  }
  if (started || cur !== "") out.push(cur);
  return out;
}

// The form's starting point and what has changed.

const SECTIONS = { args: "arg", env: "env", headers: "header" };

/** @param {string} key @param {string} value @returns {ValueRow} */
function rowFrom(key, value) {
  const mode = value === "" || value.includes("${") ? "env" : "text";
  return { id: freshId(), key, value, mode, kept: mode === "text" && detectToken(value) !== null };
}

/** @param {McpDraft} d */
function snapshot(d) {
  return {
    name: d.name, transport: d.transport, command: d.command, args: d.args.map((a) => a.value),
    env: Object.fromEntries(d.env.map((r) => [r.key, r.value])), url: d.url, headers: Object.fromEntries(d.headers.map((r) => [r.key, r.value])),
    agents: [...d.agents], projects: [...d.projects], disabledTools: [...d.disabledTools],
  };
}

/**
 * @param {Pick<Catalog, "mcpServers">} _catalog
 * @param {{ previous?: Catalog["mcpServers"][number], from?: Catalog["mcpServers"][number], name?: string }} [source]
 *        previous: the server being edited; from: a server to copy; name: a name the link suggests
 * @returns {McpDraft}
 */
export function mcpDraft(_catalog, { previous, from, name = "" } = {}) {
  const source = previous ?? from;
  /** @type {McpDraft} */
  const draft = {
    previousName: previous?.name ?? "",
    name: previous ? previous.name : name,
    transport: source ? /** @type {any} */ (source.transport) : "stdio",
    command: source?.command ?? "",
    args: (source?.args ?? []).map((value) => ({ id: freshId(), value })),
    env: Object.entries(source?.env ?? {}).map(([k, v]) => rowFrom(k, v)),
    url: source?.url ?? "",
    headers: Object.entries(source?.headers ?? {}).map(([k, v]) => rowFrom(k, v)),
    agents: previous ? [...previous.agents] : [],
    projects: previous ? previous.projects.map((p) => p.id) : [],
    knownTools: previous?.knownTools ? [...previous.knownTools] : null,
    probedAt: previous?.probedAt ?? "",
    disabledTools: previous ? [...(previous.disabledTools ?? [])] : [],
    base: {},
  };
  draft.base = previous ? snapshot(draft) : snapshot({ ...draft, name: "", transport: "stdio", command: "", args: [], env: [], url: "", headers: [], agents: [], projects: [], disabledTools: [] });
  return draft;
}

const symmetric = (a, b) => a.filter((x) => !b.includes(x)).length + b.filter((x) => !a.includes(x)).length;
const pairsChanged = (a, b) => [...new Set([...Object.keys(a), ...Object.keys(b)])].filter((k) => a[k] !== b[k]).length;

/** How many things differ from what the form started with. @param {McpDraft} d */
export function mcpChanges(d) {
  const now = snapshot(d);
  const was = /** @type {ReturnType<typeof snapshot>} */ (d.base);
  let n = 0;
  for (const f of /** @type {const} */ (["name", "transport", "command", "url"])) if (now[f] !== was[f]) n++;
  if (JSON.stringify(now.args) !== JSON.stringify(was.args)) n++;
  n += pairsChanged(now.env, was.env) + pairsChanged(now.headers, was.headers);
  return n + symmetric(now.agents, was.agents) + symmetric(now.projects, was.projects) + symmetric(now.disabledTools, was.disabledTools);
}

/** The ${VAR} references of the fields the chosen transport uses, once each, in order. @param {McpDraft} d */
export function mcpRefs(d) {
  const texts = d.transport === "stdio" ? [d.command, ...d.args.map((a) => a.value), ...d.env.map((r) => r.value)] : [d.url, ...d.headers.map((r) => r.value)];
  const names = [];
  for (const t of texts) for (const n of varNames(t)) if (!names.includes(n)) names.push(n);
  return names;
}

/** The banner that says a rename reaches the agents and projects that have the server. @param {Pick<Catalog, "mcpServers">} catalog @param {McpDraft} d */
export function rename(catalog, d) {
  if (!d.previousName) return { show: false, text: "" };
  const was = catalog.mcpServers.find((s) => s.name === d.previousName);
  const agents = was?.agents.length ?? 0;
  const projects = was?.projects.length ?? 0;
  if (agents + projects === 0) return { show: true, text: "No agent and no project has this server, so a rename changes nothing else." };
  return { show: true, text: `Renaming rewrites the name in ${plural(agents, "agent")} and ${plural(projects, "project")}. One save changes them all, or none.` };
}

// Tools: the tool default, a global enable/disable per known tool.

/** The probe time as the editor shows it, in UTC so it reads the same anywhere. @param {string} iso */
export function probedAtText(iso) {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? "" : `${new Date(t).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

/**
 * The tool list as the editor lays it out. A server never probed successfully
 * has unknown tools, all enabled, and no rows. A name in disabledTools that
 * knownTools no longer holds has no row; the save keeps it.
 * @param {Pick<McpDraft, "knownTools" | "probedAt" | "disabledTools" | "previousName">} d
 */
export function mcpTools(d) {
  const saved = d.previousName !== "";
  if (d.knownTools === null) {
    return {
      unknown: true, rows: [], refreshable: saved, probed: "",
      summary: "Tools unknown. Every tool of this server is enabled.",
      hint: saved ? "Save the server or choose Refresh tools to learn its tools." : "Save the server and the harness learns its tools.",
    };
  }
  const rows = d.knownTools.map((name) => ({ name, enabled: !d.disabledTools.includes(name) }));
  const off = rows.filter((r) => !r.enabled).length;
  const probed = probedAtText(d.probedAt);
  return {
    unknown: false, rows, refreshable: saved, probed: probed ? `Last probed ${probed}.` : "",
    summary: rows.length === 0 ? "The server lists no tools." : `${plural(rows.length, "tool")}, ${off === 0 ? "all enabled" : `${off} disabled`}.`,
    hint: rows.length > 0 ? "A disabled tool is hidden from every project that does not set it itself." : "",
  };
}

/** Flips one tool of the default: enabled becomes disabled and back. @param {McpDraft} d @param {string} name */
export function toggleTool(d, name) {
  d.disabledTools = d.disabledTools.includes(name) ? d.disabledTools.filter((t) => t !== name) : [...d.disabledTools, name];
}

/**
 * Takes the tool data of a refreshed catalog entry and nothing else: the
 * definition the person is editing and the toggles they set stay as they are.
 * @param {McpDraft} d @param {{ knownTools: string[] | null, probedAt: string }} entry
 */
export function adoptTools(d, entry) {
  d.knownTools = entry.knownTools ? [...entry.knownTools] : null;
  d.probedAt = entry.probedAt ?? "";
}

/**
 * What a probe outcome says: after a save (the server is saved either way) or
 * after Refresh tools. `tools` is the entry's known tools once it is read.
 * @param {{ status: "ok"|"error", error: string, hint: string } | null | undefined} probe
 * @param {{ tools: string[] | null, saved?: boolean }} o  saved: the probe ran with a save
 * @returns {{ show: boolean, tone: "" | "attention", text: string }}
 */
export function probeOutcome(probe, { tools, saved = false }) {
  if (!probe) return { show: false, tone: "", text: "" };
  if (probe.status === "ok") {
    const n = tools?.length ?? 0;
    return { show: true, tone: "", text: n === 0 ? "Probed the server: it lists no tools." : `Probed the server: ${plural(n, "tool")} found.` };
  }
  const why = probe.error || "The server did not connect.";
  const kept = tools === null ? "Its tools stay unknown." : "Its last known tools are kept.";
  return { show: true, tone: "attention", text: `${saved ? "The server is saved, but probing it failed" : "Probing the server failed"}: ${why}${probe.hint ? ` ${probe.hint}` : ""} ${kept}` };
}

// Rows.

/** @param {McpDraft} d @param {"args"|"env"|"headers"} section */
export function addRow(d, section) {
  if (section === "args") d.args.push({ id: freshId(), value: "" });
  else d[section].push({ id: freshId(), key: "", value: "", mode: "env", kept: false });
}

/** @param {McpDraft} d @param {"args"|"env"|"headers"} section @param {number} id */
export function removeRow(d, section, id) {
  d[section] = /** @type {any[]} */ (d[section]).filter((r) => r.id !== id);
}

/**
 * Switches a row between From env and Plain text. To From env, a token or an
 * empty value becomes the reference; an ordinary value is left for the person.
 * @param {McpDraft} d @param {"env"|"headers"} section @param {number} id @param {"env"|"text"} mode
 */
export function setMode(d, section, id, mode) {
  const row = d[section].find((r) => r.id === id);
  if (!row) return;
  row.mode = mode;
  row.kept = false;
  if (mode !== "env") return;
  const name = referenceName({ kind: SECTIONS[section], key: row.key, server: d.name });
  if (detectToken(row.value)) row.value = useReference(row.value, name);
  else if (row.value === "") row.value = "${" + name + "}";
}

/** Every token the form holds as plain text, so it can be hidden wherever the page shows the server's answer. @param {McpDraft} d */
function secretValues(d) {
  const found = [];
  for (const v of [...d.args.map((a) => a.value), ...d.env.map((r) => r.value), ...d.headers.map((r) => r.value)]) {
    const t = detectToken(v);
    if (t) found.push(t.secret);
  }
  return [...new Set(found)].sort((a, b) => b.length - a.length);
}

// Checks.

const VAR_KEY = /^[A-Za-z_][A-Za-z0-9_]*$/;
const HEADER_KEY = /^[A-Za-z0-9!#$%&'*+.^_`|~-]+$/;

/** A free name near `name`: name-2, name-3 … @param {string} name @param {Set<string>} taken */
function suggest(name, taken) {
  for (let i = 2; ; i++) {
    const tail = `-${i}`;
    const candidate = `${name.slice(0, 64 - tail.length).replace(/-+$/, "")}${tail}`;
    if (!taken.has(candidate)) return candidate;
  }
}

/** What is wrong with a list of key and value rows, or "". */
function rowsProblem(rows, { kind, validKey, badKey }) {
  const seen = new Set();
  for (const r of rows) {
    const key = r.key.trim();
    if (key === "" && r.value === "") continue;
    if (key === "") return `A ${kind} with a value needs a name.`;
    if (!validKey.test(key)) return `${key} is not a ${badKey}.`;
    const id = kind === "header" ? key.toLowerCase() : key;
    if (seen.has(id)) return `${key} is listed twice.`;
    seen.add(id);
    const bad = placeholderProblem(r.value);
    if (bad) return bad;
  }
  return "";
}

/**
 * Whether the form is fit to send and what is wrong with it at once. A missing
 * name, command or URL shows only once the person has touched the form. A
 * token in a plain value blocks it until the person settles the warning.
 * @param {McpDraft} draft
 * @param {Pick<Catalog, "mcpServers">} catalog
 * @param {{ touched?: boolean }} [o]
 */
export function checkMcpDraft(draft, catalog, { touched = false } = {}) {
  const name = draft.name;
  let nameProblem = name === "" ? (touched ? "Server name is required." : "") : mcpNameProblem(name);
  /** @type {{ name: string, href: string, suggestion: string } | null} */
  let taken = null;
  if (name !== "" && nameProblem === "" && name !== draft.previousName) {
    const names = new Set(catalog.mcpServers.map((s) => s.name));
    if (names.has(name)) {
      taken = { name, href: `/agents/mcp/${encodeURIComponent(name)}`, suggestion: suggest(name, names) };
      nameProblem = `A server named ${name} already exists.`;
    }
  }
  const local = draft.transport === "stdio";
  let commandProblem = "";
  let urlProblem = "";
  let argsProblem = "";
  let envProblem = "";
  let headersProblem = "";
  if (local) {
    if (draft.command.trim() === "") commandProblem = touched ? "Command is required for a local server." : "";
    else commandProblem = placeholderProblem(draft.command);
    for (const a of draft.args) argsProblem ||= placeholderProblem(a.value);
    envProblem = rowsProblem(draft.env, { kind: "variable", validKey: VAR_KEY, badKey: "variable name: use letters, digits and underscores, not starting with a digit" });
  } else {
    const url = draft.url.trim();
    if (url === "") urlProblem = touched ? "URL is required for a remote server." : "";
    else if (!/^(https?:\/\/|\$\{)/i.test(url)) urlProblem = "The URL starts with http:// or https://.";
    else urlProblem = placeholderProblem(url);
    headersProblem = rowsProblem(draft.headers, { kind: "header", validKey: HEADER_KEY, badKey: "header name: use letters, digits and hyphens" });
  }

  const sections = local ? /** @type {const} */ (["args", "env"]) : /** @type {const} */ (["headers"]);
  const secrets = [];
  for (const section of sections) {
    for (const row of draft[section]) {
      const found = detectToken(row.value);
      if (!found) continue;
      const key = "key" in row ? row.key : "";
      secrets.push({ section, id: row.id, state: "kept" in row && row.kept ? "kept" : "warn", kind: found.kind, variable: referenceName({ kind: SECTIONS[section], key, server: name }) });
    }
  }
  const blocked = secrets.some((s) => s.state === "warn");
  const targetReady = local ? draft.command.trim() !== "" : draft.url.trim() !== "";
  const fine = [nameProblem, commandProblem, urlProblem, argsProblem, envProblem, headersProblem].every((p) => p === "");
  return {
    valid: name !== "" && targetReady && fine && !blocked,
    nameProblem, taken, commandProblem, urlProblem, argsProblem, envProblem, headersProblem,
    secrets, blocked,
    /** Whether a connection test makes sense: the target is there and no token waits for a choice. */
    testable: targetReady && commandProblem === "" && urlProblem === "" && argsProblem === "" && envProblem === "" && headersProblem === "" && !blocked,
  };
}

// What the save is given.

/**
 * @param {McpDraft} d
 * @param {{ unattached?: boolean }} [o]  unattached: the drawer's save, which gives the server to nobody
 */
export function mcpSave(d, { unattached = false } = {}) {
  const local = d.transport === "stdio";
  const pairs = (rows) => Object.fromEntries(rows.filter((r) => r.key.trim() !== "" && (r.value !== "" || r.mode === "text")).map((r) => [r.key.trim(), r.value]));
  return {
    previousName: d.previousName || undefined,
    server: {
      name: d.name.trim(),
      transport: d.transport,
      command: local ? d.command.trim() : "",
      args: local ? d.args.map((a) => a.value).filter((v) => v !== "") : [],
      env: local ? pairs(d.env) : {},
      url: local ? "" : d.url.trim(),
      headers: local ? {} : pairs(d.headers),
      disabledTools: [...d.disabledTools],
    },
    agents: unattached ? [] : [...d.agents],
    projects: unattached ? [] : [...d.projects],
  };
}

// What will be written.

/**
 * The server's answer, with every token the form holds hidden: the page shows
 * the answer, and a secret never shows. The answer itself is not changed.
 * @template {{ summary: string, writes: { path: string, before: string | null, after: string | null, encoding?: string }[], projects: object[] }} T
 * @param {T} answer @param {McpDraft} d @returns {T}
 */
export function hideSecrets(answer, d) {
  const secrets = secretValues(d);
  if (secrets.length === 0) return answer;
  const hide = (text) => (typeof text === "string" ? secrets.reduce((t, s) => t.split(s).join(HIDDEN), text) : text);
  return { ...answer, summary: hide(answer.summary), writes: answer.writes.map((w) => ({ ...w, before: hide(w.before), after: hide(w.after) })) };
}

/**
 * One panel per file of the dry run, and one per project list it sets.
 * @param {{ writes: Parameters<typeof previewModel>[0], projects: { id: string, name: string, before: string[], after: string[] }[] }} change
 */
export function mcpPreviewModel({ writes, projects }) {
  const files = previewModel(writes);
  const lists = projects.map((p) => ({
    key: `project:${p.id}`, path: `project “${p.name}”`, badge: "saved with the project", isText: true, isDiff: false,
    text: `mcp_servers: [${p.after.length ? ` ${p.after.map((n) => JSON.stringify(n)).join(", ")} ` : ""}]`, lines: [],
  }));
  return [...files, ...lists];
}

/** Who gets the server after the save, in the words of the banner and the footer. @param {Pick<McpDraft, "agents" | "projects">} d */
export function whoGetsIt(d) {
  const agents = d.agents.length;
  const projects = d.projects.length;
  if (agents + projects === 0) {
    return { agents, projects, unused: true, text: "Unused. No agent and no project gets this server yet. That is allowed.", short: "Unused: no agent and no project gets it yet." };
  }
  const who = `${plural(agents, "agent")} and ${plural(projects, "project")}`;
  return { agents, projects, unused: false, text: `Reaches ${who}, from their next session. Running sessions keep what they started with.`, short: `Reaches ${who} from their next session.` };
}

// Refusals.

const FIELDS = new Set(["name", "command", "url", "env", "headers", "args"]);

/**
 * Which form field a refusal belongs to.
 * @param {import("./refusals.js").Refusal | null} refusal
 */
export function mcpFieldErrors(refusal) {
  const out = { name: "", command: "", url: "", env: "", headers: "", args: "", form: "", suggestion: "" };
  if (!refusal) return out;
  if (refusal.code === "NAME_TAKEN") {
    out.name = refusal.message;
    out.suggestion = refusal.suggestion;
    return out;
  }
  const field = refusal.code === "INVALID_INPUT" ? refusal.field.split(".").pop() ?? "" : "";
  out[FIELDS.has(field) ? field : "form"] = refusal.message;
  return out;
}

// The connection test.

const SHOWN_TOOLS = 6;

/** @param {import("./ports.js").Probe | null} probe */
export function probeView(probe) {
  if (!probe) return { phase: "idle", tone: "", title: "", detail: "", tools: /** @type {string[]} */ ([]), more: 0, hint: "", canSave: "" };
  if (probe.status === "ok") {
    const n = probe.tools.length;
    return {
      phase: "ok", tone: "success", detail: "", hint: "", canSave: "",
      title: n === 0 ? "Connected. The server lists no tools." : `Connected. ${plural(n, "tool")}:`,
      tools: probe.tools.slice(0, SHOWN_TOOLS).map((t) => t.name),
      more: Math.max(0, n - SHOWN_TOOLS),
    };
  }
  return {
    phase: "error", tone: "danger", title: probe.error || "The server did not connect.", detail: "", tools: [], more: 0, hint: probe.hint,
    canSave: "You can still save; agents will see the server fail to start.",
  };
}
