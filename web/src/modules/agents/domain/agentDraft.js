// What the agent editor needs (UX spec section 4): the form's starting point,
// the checks the page can make at once, the resolved skills of the entries
// chosen (computed here so the summary follows every keystroke; the server
// resolves them for real when it saves), the skill-and-bundle picker's options,
// the MCP server and tool choices, and where a refusal lands.

import { projectsChanged } from "./enabledIn.js";
import { slugProblem } from "./names.js";

/** @typedef {import("./ports.js").Catalog} Catalog */
/** @typedef {{ previousName: string, name: string, description: string, aliases: string[], entries: string[], mcpServers: string[], allowedTools: string[], projects: string[] | null }} AgentDraft
 *   projects: the Selected projects ticked as listing the agent, or null while the section is untouched (the catalog says, and a save leaves the lists alone) */

/** The picker option that opens the skill form. */
export const CREATE_SKILL = "+create";
/** How many options the picker mounts at once. */
export const AGENT_PICKER_PAGE = 50;

const enc = encodeURIComponent;
const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/**
 * The form's starting point: the agent as the catalog read it, an empty one, or
 * (with `from`) a copy of another agent under no name.
 * @param {Catalog} catalog
 * @param {string} name  "" for a new agent
 * @param {string} [from]  an agent to copy
 * @returns {AgentDraft | null}  null when `name` is not an agent
 */
export function agentDraft(catalog, name, from = "") {
  const copy = (a) => ({ description: a.description, aliases: [...a.aliases], entries: [...a.entries], mcpServers: [...a.mcpServers], allowedTools: [...a.allowedTools] });
  if (!name) {
    const source = from ? catalog.agents.find((a) => a.name === from) : undefined;
    return { previousName: "", name: "", ...(source ? copy(source) : { description: "", aliases: [], entries: [], mcpServers: [], allowedTools: [] }), projects: null };
  }
  const a = catalog.agents.find((x) => x.name === name);
  return a ? { previousName: a.name, name: a.name, ...copy(a), projects: null } : null;
}

/**
 * Whether the form is fit to send and what is wrong with it at once. The
 * description's message shows only once the person has touched the form (or
 * the agent was already in the catalog), so a new form does not scold.
 * @param {AgentDraft} draft
 * @param {{ touched?: boolean }} [o]
 */
export function checkAgentDraft(draft, { touched = false } = {}) {
  const nameProblem = draft.name === "" ? "" : slugProblem(draft.name, "Agent name");
  const noDescription = draft.description.trim() === "";
  return {
    valid: draft.name !== "" && nameProblem === "" && !noDescription,
    nameProblem,
    descriptionProblem: noDescription && touched ? "Add a description to save." : "",
  };
}

const same = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

/**
 * How many fields differ from what the editor read, each counted once.
 * @param {Catalog} catalog
 * @param {AgentDraft} draft
 */
export function agentChanges(catalog, draft) {
  const read = draft.previousName ? catalog.agents.find((a) => a.name === draft.previousName) : undefined;
  const base = read
    ? { name: read.name, description: read.description, aliases: read.aliases, entries: read.entries, mcpServers: read.mcpServers, allowedTools: read.allowedTools }
    : { name: "", description: "", aliases: [], entries: [], mcpServers: [], allowedTools: [] };
  let n = 0;
  if (draft.name !== base.name) n++;
  if (draft.description !== base.description) n++;
  for (const k of ["aliases", "entries", "mcpServers", "allowedTools"]) if (!same(draft[k], base[k])) n++;
  if (projectsChanged(catalog, draft)) n++;
  return n;
}

/**
 * The skills the entries come to, as the server resolves them: each bundle
 * expands in place (nested ones too), and the first time a skill appears is the
 * one that stays.
 * @param {Pick<Catalog, "bundles" | "skills">} catalog
 * @param {string[]} entries
 * @returns {{ resolved: string[], dropped: string[], missing: string[] }}
 */
export function expandEntries(catalog, entries) {
  const bundles = new Map(catalog.bundles.map((b) => [b.name, b]));
  const skills = new Set(catalog.skills.map((s) => s.name));
  const resolved = [];
  const dropped = [];
  const missing = [];
  const walk = (list, path) => {
    for (const e of list) {
      if (e.startsWith("@")) {
        const b = bundles.get(e.slice(1));
        if (!b) {
          if (!missing.includes(e)) missing.push(e);
        } else if (!path.includes(b.name)) {
          walk(b.entries, [...path, b.name]);
        }
      } else if (!skills.has(e)) {
        if (!missing.includes(e)) missing.push(e);
      } else if (resolved.includes(e)) {
        if (!dropped.includes(e)) dropped.push(e);
      } else {
        resolved.push(e);
      }
    }
  };
  walk(entries, []);
  return { resolved, dropped, missing };
}

/**
 * The line under the chips.
 * @param {Pick<Catalog, "bundles" | "skills">} catalog
 * @param {string[]} entries
 */
export function resolvedSummary(catalog, entries) {
  const { resolved, dropped, missing } = expandEntries(catalog, entries);
  const total = resolved.length;
  if (entries.length === 0) return { total, dropped, missing, text: "No skills yet" };
  const parts = [`${plural(total, "skill")} after bundles are expanded`];
  if (dropped.length) parts.push(`${plural(dropped.length, "duplicate")} dropped (${dropped.join(", ")})`);
  if (missing.length) parts.push(`${missing.length} missing (${missing.join(", ")})`);
  return { total, dropped, missing, text: parts.join(" · ") };
}

/**
 * The picker's options for a query: bundles first, then skills, then the ways
 * to make a skill. A skill a chosen bundle already brings says so.
 * @param {Pick<Catalog, "bundles" | "skills">} catalog
 * @param {{ query: string, entries: string[], limit?: number }} o
 */
export function agentPickerOptions(catalog, { query, entries, limit = AGENT_PICKER_PAGE }) {
  const typed = query.trim();
  const q = typed.toLowerCase().replace(/^@/, "");
  const chosen = new Set(entries);
  const byName = (a, b) => a.name.localeCompare(b.name);

  const via = new Map();
  for (const e of entries.filter((x) => x.startsWith("@"))) {
    for (const s of expandEntries(catalog, [e]).resolved) if (!via.has(s)) via.set(s, e);
  }

  const bundles = [...catalog.bundles].sort(byName)
    .filter((b) => !q || b.name.toLowerCase().includes(q))
    .map((b) => ({ key: `@${b.name}`, kind: "bundle", label: `@${b.name}`, hint: plural(expandEntries(catalog, [`@${b.name}`]).resolved.length, "skill"), chosen: chosen.has(`@${b.name}`) }));
  const skills = [...catalog.skills].sort(byName)
    .filter((s) => !q || s.name.toLowerCase().includes(q) || (s.description ?? "").toLowerCase().includes(q))
    .map((s) => ({ key: s.name, kind: "skill", label: s.name, hint: via.has(s.name) ? `already in ${via.get(s.name)}` : "", chosen: chosen.has(s.name) }));
  const found = [...bundles, ...skills];
  const create = { key: CREATE_SKILL, kind: "create", label: found.length === 0 && typed ? `Create skill “${typed}”…` : "Create a new skill…", hint: "", chosen: false };
  return { options: [...found.slice(0, limit), create], hidden: Math.max(0, found.length - limit), empty: false };
}

/**
 * The skill form's address, with the editor to come back to.
 * @param {string} returnTo  the editor's path, query and hash
 * @param {string} query  what the person typed, "" for none
 */
export function createSkillHref(returnTo, query) {
  const name = query.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 64);
  return `/agents/skills/new?${name ? `name=${enc(name)}&` : ""}return=${enc(returnTo)}`;
}

/**
 * The checklist of catalog servers, each with its transport and whether each
 * ${VAR} is set. A server the agent names that the catalog lacks stays in, so
 * the person can untick it.
 * @param {Pick<Catalog, "mcpServers">} catalog
 * @param {string[]} chosen
 */
export function mcpChoices(catalog, chosen) {
  const on = new Set(chosen);
  const rows = catalog.mcpServers.map((s) => ({
    name: s.name,
    checked: on.has(s.name),
    transport: s.transport,
    missing: false,
    refs: s.envRefs.map((r) => ({ label: "${" + r.name + "}", state: r.set ? "set" : "not set", tone: r.set ? "success" : "attention" })),
  }));
  const known = new Set(catalog.mcpServers.map((s) => s.name));
  for (const n of chosen) if (!known.has(n)) rows.push({ name: n, checked: true, transport: "", missing: true, refs: [] });
  return rows;
}

/**
 * The checklist of built-in tools and the line under it.
 * @param {Pick<Catalog, "builtInTools">} catalog
 * @param {string[]} chosen
 */
export function toolChoices(catalog, chosen) {
  const on = new Set(chosen);
  const rows = catalog.builtInTools.map((t) => ({ name: t.name, description: t.description, checked: on.has(t.name) }));
  const known = new Set(catalog.builtInTools.map((t) => t.name));
  for (const n of chosen) if (!known.has(n)) rows.push({ name: n, description: "Not one of Claude Code’s built-in tools.", checked: true });
  return {
    rows,
    summary: chosen.length ? `${chosen.length} allowed · everything else is not allowed to this agent` : "None ticked · sessions get Claude’s default tools",
  };
}

/** What each section of the contents list counts. @param {Catalog} catalog @param {AgentDraft} draft @param {string} [projects]  the Projects section's own count (enabledIn.js) */
export function tocCounts(catalog, draft, projects = "") {
  return {
    skills: String(expandEntries(catalog, draft.entries).resolved.length),
    mcp: String(draft.mcpServers.length),
    tools: draft.allowedTools.length ? String(draft.allowedTools.length) : "default",
    projects,
  };
}

const FIELD_OF = { name: "name", description: "description", aliases: "aliases", entries: "entries", skills: "entries", mcp_servers: "mcp", allowed_tools: "tools", projects: "projects" };

/**
 * Which form field a refusal belongs to. An UNKNOWN_REFERENCE names things
 * that are skills or servers, so each name goes to the list that holds it.
 * @param {import("./refusals.js").Refusal | null} refusal
 * @param {AgentDraft} draft
 */
export function agentFieldErrors(refusal, draft) {
  const out = { name: "", description: "", aliases: "", entries: "", mcp: "", tools: "", projects: "", form: "" };
  if (!refusal) return out;
  if (refusal.code === "UNKNOWN_REFERENCE") {
    const say = (names) => `Not in the catalog: ${names.join(", ")}. Remove ${names.length === 1 ? "it" : "them"} and save again.`;
    const inEntries = refusal.names.filter((n) => draft.entries.includes(n));
    const inServers = refusal.names.filter((n) => draft.mcpServers.includes(n) && !inEntries.includes(n));
    if (inEntries.length) out.entries = say(inEntries);
    if (inServers.length) out.mcp = say(inServers);
    if (!inEntries.length && !inServers.length) out.form = refusal.message;
    return out;
  }
  const field = refusal.code === "NAME_TAKEN" ? "name" : refusal.code === "INVALID_INPUT" ? FIELD_OF[refusal.field] : undefined;
  out[field ?? "form"] = refusal.message;
  return out;
}
