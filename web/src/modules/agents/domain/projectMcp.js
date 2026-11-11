// What a project's settings show of the catalog's MCP servers (UX spec
// section 7): a row per server with its project MCP state (Added, Agent's
// choice, Banned) and, for known tools, a tool list with the tool override and
// what is in effect; the draft compared with what is saved; the servers a
// catalog change took off the project; and the What a session gets example. A
// variable's value is never here, nor a plain env or header value: only the
// ${VAR} references and whether each is set.

import { varNames } from "./mcp.js";

/** @typedef {import("./ports.js").Catalog} Catalog */

const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;

function whoElse(server, projectId) {
  const others = server.projects.filter((p) => p.id !== projectId).length;
  if (server.agents.length === 0 && others === 0) return "Unused";
  const parts = [];
  if (server.agents.length > 0) parts.push(plural(server.agents.length, "agent"));
  if (others > 0) parts.push(`${plural(others, "other project")}`);
  return `Used by ${parts.join(" and ")}`;
}

// A project's MCP settings (contract: project MCP bans and tool overrides,
// section 2). `settings` is what the project has saved; `draft` is what the
// page shows now: { [server]: { state, overrides: { [tool]: "enabled" | "disabled" } } }.
// A server or tool the draft does not name is Agent's choice and Default, as the
// server reads it.

export const STATES = /** @type {const} */ (["added", "agent", "banned"]);
const STATE_LABEL = { added: "Added", agent: "Agent’s choice", banned: "Banned" };
const STATE_HINT = {
  added: "Every session in the project gets it.",
  agent: "Only sessions whose agent lists it get it.",
  banned: "No session in the project gets it.",
};
const CHOICES = /** @type {const} */ (["default", "enabled", "disabled"]);
const cap = (word) => word.charAt(0).toUpperCase() + word.slice(1);

/** @typedef {{ state: "added" | "agent" | "banned", overrides: Record<string, "enabled" | "disabled"> }} ServerDraft */
/** @typedef {Record<string, ServerDraft>} McpDraft */

/** The project's saved settings as the page starts from them. @param {import("./ports.js").ProjectMcpSettings} settings @returns {McpDraft} */
export function settingsDraft(settings) {
  return Object.fromEntries(settings.servers.map((s) => [s.name, {
    state: s.state,
    overrides: Object.fromEntries((s.tools ?? []).filter((t) => t.override !== "default").map((t) => [t.name, /** @type {"enabled" | "disabled"} */ (t.override)])),
  }]));
}

/** @param {McpDraft} draft @param {string} name @returns {ServerDraft} */
const entry = (draft, name) => draft[name] ?? { state: "agent", overrides: {} };

/** The draft with one server's state set; its overrides stay (a banned server keeps them for when it comes back). @param {McpDraft} draft */
export function setServerState(draft, name, state) {
  return { ...draft, [name]: { ...entry(draft, name), state } };
}

/** The draft with one tool set to Default, Enabled or Disabled for this project. @param {McpDraft} draft @param {"default" | "enabled" | "disabled"} choice */
export function setToolOverride(draft, name, tool, choice) {
  const rest = Object.fromEntries(Object.entries(entry(draft, name).overrides).filter(([t]) => t !== tool));
  return { ...draft, [name]: { ...entry(draft, name), overrides: choice === "default" ? rest : { ...rest, [tool]: choice } } };
}

/** Reset to defaults: every override of the server goes back to Default. @param {McpDraft} draft */
export function resetServerOverrides(draft, name) {
  return { ...draft, [name]: { ...entry(draft, name), overrides: {} } };
}

const sameOverrides = (a, b) => {
  const ka = Object.keys(a);
  return ka.length === Object.keys(b).length && ka.every((k) => a[k] === b[k]);
};

/** The servers whose state or overrides differ from what is saved. @param {import("./ports.js").ProjectMcpSettings} settings @param {McpDraft} draft */
export function changedServers(settings, draft) {
  const saved = settingsDraft(settings);
  return settings.servers.map((s) => s.name).filter((n) => entry(saved, n).state !== entry(draft, n).state || !sameOverrides(entry(saved, n).overrides, entry(draft, n).overrides));
}

/** Whether the draft differs from what is saved: states and overrides. @param {import("./ports.js").ProjectMcpSettings} settings @param {McpDraft} draft */
export function settingsChanged(settings, draft) {
  return changedServers(settings, draft).length > 0;
}

/**
 * The settings read again (the catalog changed, or Refresh tools): what is
 * saved now is the new read, and a server the person changed keeps their
 * state and overrides; every other server takes the new read.
 * @param {import("./ports.js").ProjectMcpSettings} before @param {McpDraft} draft @param {import("./ports.js").ProjectMcpSettings} after
 * @returns {McpDraft}
 */
export function mergeSettings(before, draft, after) {
  const mine = new Set(changedServers(before, draft));
  const fresh = settingsDraft(after);
  return Object.fromEntries(after.servers.map((s) => [s.name, mine.has(s.name) ? entry(draft, s.name) : fresh[s.name]]));
}

/**
 * What set_project_mcp_settings is given: every server the project's read
 * lists, with its state and its overrides (Default ones left out).
 * @param {import("./ports.js").ProjectMcpSettings} settings @param {McpDraft} draft
 * @returns {{ name: string, state: string, toolOverrides: Record<string, string> }[]}
 */
export function settingsToWrite(settings, draft) {
  return settings.servers.map((s) => ({ name: s.name, state: entry(draft, s.name).state, toolOverrides: { ...entry(draft, s.name).overrides } }));
}

/** The names of the servers in a state. @param {import("./ports.js").ProjectMcpSettings} settings @param {McpDraft} draft @param {string} state */
export function namesInState(settings, draft, state) {
  return settings.servers.map((s) => s.name).filter((n) => entry(draft, n).state === state);
}

/** The Added servers' names, in the project's order of the catalog. @param {import("./ports.js").ProjectMcpSettings} settings */
export function addedNames(settings) {
  return settings.servers.filter((s) => s.state === "added").map((s) => s.name);
}

/** The servers a catalog change can take off the project: the ones it has set (Added or Banned). @param {import("./ports.js").ProjectMcpSettings} settings */
export function setNames(settings) {
  return settings.servers.filter((s) => s.state !== "agent").map((s) => s.name);
}

/** The count line above the rows. @param {import("./ports.js").ProjectMcpSettings} settings @param {McpDraft} draft */
export function settingsCount(settings, draft) {
  const n = settings.servers.length;
  if (n === 0) return "";
  const of = (state) => namesInState(settings, draft, state).length;
  return `${plural(n, "server")}: ${of("added")} Added, ${of("agent")} Agent’s choice, ${of("banned")} Banned`;
}

/** The toast phrase for what a save gave. @param {import("./ports.js").ProjectMcpSettings} settings */
export function settingsPhrase(settings, kind = "saved") {
  const added = addedNames(settings).length;
  const banned = settings.servers.filter((s) => s.state === "banned").length;
  const gives = `gives ${plural(added, "MCP server")}${kind === "saved" ? " to its sessions" : ""}`;
  return banned > 0 ? `${gives} and bans ${banned}` : gives;
}

/**
 * The ban warning: it names the agents that list the server and refuses nothing.
 * @param {string[]} agents
 */
export function banWarning(agents) {
  const head = "Banned: no session in this project gets this server, whatever its agent lists.";
  if (agents.length === 0) return `${head} No agent lists it, so no agent loses anything here.`;
  if (agents.length === 1) return `${head} The agent that lists it, ${agents[0]}, loses it in this project.`;
  return `${head} The ${agents.length} agents that list it, ${agents.join(", ")}, lose it in this project.`;
}

/**
 * The tool list under a server: a row per known tool with the choice made and
 * what is in effect, or the words for tools never learned.
 * @param {import("./ports.js").ProjectMcpServerSettings} server @param {ServerDraft} mine
 */
export function toolRows(server, mine) {
  if (server.tools === null) {
    return { unknown: true, text: "Tools unknown: this server was never probed, so every tool is enabled. Choose Refresh tools to learn them.", rows: [], summary: "" };
  }
  const rows = server.tools.map((t) => {
    const choice = mine.overrides[t.name] ?? "default";
    const effective = choice === "default" ? t.default : choice;
    return {
      name: t.name,
      aria: `${t.name} in this project`,
      choice,
      effective,
      effectiveText: cap(effective),
      source: choice === "default" ? "from the default" : "set for this project",
      choices: CHOICES.map((value) => ({ value, label: value === "default" ? `Default (${t.default})` : cap(value), pressed: value === choice })),
    };
  });
  const off = rows.filter((r) => r.effective === "disabled").length;
  const summary = rows.length === 0 ? "The server lists no tools." : `${plural(rows.length, "tool")}, ${off === 0 ? "all enabled" : `${off} disabled`} here`;
  return { unknown: false, text: "", rows, summary };
}

/**
 * @param {Catalog} catalog
 * @param {string} projectId
 * @param {import("./ports.js").ProjectMcpSettings} settings
 * @param {McpDraft} draft
 */
export function projectMcpRows(catalog, projectId, settings, draft) {
  const rows = [];
  for (const server of settings.servers) {
    const s = catalog.mcpServers.find((x) => x.name === server.name);
    if (!s) continue;
    const mine = entry(draft, s.name);
    const known = new Map(s.envRefs.map((r) => [r.name, r.set]));
    const names = [];
    for (const text of [s.command, ...s.args, s.url, ...Object.values(s.env), ...Object.values(s.headers)]) {
      for (const n of varNames(String(text))) if (!names.includes(n)) names.push(n);
    }
    const vars = names.map((n) => {
      const set = known.get(n) ?? false;
      return { label: "${" + n + "}", state: set ? "set" : "not set", tone: set ? "success" : "attention" };
    });
    const unset = vars.some((v) => v.tone === "attention");
    const tools = toolRows(server, mine);
    const banned = mine.state === "banned";
    rows.push({
      name: s.name,
      transport: s.builtIn ? "built-in" : s.transport,
      builtIn: s.builtIn,
      state: mine.state,
      stateLabel: STATE_LABEL[mine.state],
      stateHint: STATE_HINT[mine.state],
      stateAria: `${s.name} in this project`,
      choices: STATES.map((value) => ({ value, label: STATE_LABEL[value], pressed: value === mine.state })),
      vars,
      who: whoElse(s, projectId),
      warning: unset ? "the server fails to start in a session until the variable is set" : "",
      warningText: unset ? " · the server fails to start in a session until the variable is set" : "",
      banned,
      banWarning: banned ? banWarning(server.usedByAgents) : "",
      tools,
      // A banned server is not attached, so its tools have nothing to show; its overrides stay for when it comes back.
      showTools: !banned,
      hasToolRows: !banned && !tools.unknown,
      toolsUnknown: !banned && tools.unknown,
      canReset: Object.keys(mine.overrides).length > 0,
    });
  }
  return rows;
}

/**
 * The servers the project had set (setNames) that the catalog no longer holds.
 * When the new names hold one the old ones lacked, a server was renamed (the
 * server rewrites the project's settings): which name went cannot be told, so
 * none is named.
 * @param {string[]} before  the project's set servers before the read
 * @param {Catalog} catalog  the catalog read now
 * @param {string[]} [after]  the project's set servers read now
 */
export function removedFromProject(before, catalog, after = before) {
  if (after.some((n) => !before.includes(n))) return [];
  const have = new Set(catalog.mcpServers.map((s) => s.name));
  return before.filter((n) => !have.has(n));
}

/**
 * The What a session gets example: an agent that has servers (else the first
 * agent) with the project's Added ones, and a session with no agent. A banned
 * server is in neither.
 * @param {Catalog} catalog
 * @param {import("./ports.js").ProjectMcpSettings} settings
 * @param {McpDraft} draft
 */
export function sessionExample(catalog, settings, draft) {
  const a = catalog.agents.find((x) => x.mcpServers.length > 0) ?? catalog.agents[0];
  const have = new Set(catalog.mcpServers.map((s) => s.name));
  const mine = namesInState(settings, draft, "added").filter((n) => have.has(n));
  const banned = namesInState(settings, draft, "banned").filter((n) => have.has(n));
  const own = (a ? a.mcpServers : []).filter((n) => !banned.includes(n));
  const withAgent = [
    ...own.map((name) => ({ name, origin: mine.includes(name) ? "both, counted once" : "agent" })),
    ...mine.filter((n) => !own.includes(n)).map((name) => ({ name, origin: "project" })),
  ];
  return { agent: a?.name ?? "", withAgent, withoutAgent: mine, banned };
}
