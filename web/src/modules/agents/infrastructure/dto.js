// The wire format of the agent catalog (contract sections 3 and 4), decoded
// to the camelCase shapes of domain/ports.js and encoded back for writes.
// Go's nil slices arrive as null: a missing list is an empty one.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const bad = (what) => {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected catalog data: ${what}.`);
};

const list = (v, what) => {
  if (v === undefined || v === null) return [];
  if (!Array.isArray(v)) bad(`${what} is not a list`);
  return v;
};
const strings = (v, what) => list(v, what).map(String);
const object = (v) => (v && typeof v === "object" && !Array.isArray(v) ? v : {});

const problems = (v) => list(v, "problems").map((p) => ({ code: String(p?.code ?? ""), message: String(p?.message ?? "") }));

/** An agent's usage {sessions, zones}; null while the server sends none. */
const usage = (u) => (u && typeof u === "object" && !Array.isArray(u) ? { sessions: Number(u.sessions ?? 0), zones: Number(u.zones ?? 0) } : null);

/** One catalog MCP server; known_tools null is "tools unknown", [] is a server with no tools. @returns {import("../domain/ports.js").CatalogMcpServer} */
function toMcpServer(m) {
  return {
    name: String(m?.name ?? ""),
    transport: String(m?.transport ?? ""),
    command: String(m?.command ?? ""),
    args: strings(m?.args, "args"),
    env: object(m?.env),
    url: String(m?.url ?? ""),
    headers: object(m?.headers),
    envRefs: list(m?.env_refs, "env_refs").map((r) => ({ name: String(r?.name ?? ""), set: r?.set === true })),
    builtIn: m?.built_in === true,
    tools: strings(m?.tools, "tools"),
    knownTools: m?.known_tools === undefined || m?.known_tools === null ? null : strings(m.known_tools, "known_tools"),
    probedAt: typeof m?.probed_at === "string" ? m.probed_at : "",
    disabledTools: strings(m?.disabled_tools, "disabled_tools"),
    agents: strings(m?.agents, "agents"),
    projects: list(m?.projects, "projects").map((p) => ({ id: String(p?.id ?? ""), name: String(p?.name ?? "") })),
    problems: problems(m?.problems),
  };
}

/** @returns {import("../domain/ports.js").Catalog} */
export function toCatalog(body) {
  if (!body || typeof body !== "object" || Array.isArray(body)) bad("expected an object");
  const broken = body.broken;
  return {
    root: typeof body.root === "string" ? body.root : "",
    versions: object(body.versions),
    broken: broken ? { path: String(broken.path ?? ""), line: Number(broken.line ?? 0), column: Number(broken.column ?? 0), message: String(broken.message ?? "") } : null,
    agents: list(body.agents, "agents").map((a) => ({
      name: String(a?.name ?? ""),
      description: String(a?.description ?? ""),
      aliases: strings(a?.aliases, "aliases"),
      entries: strings(a?.entries, "entries"),
      resolvedSkills: strings(a?.resolved_skills, "resolved_skills"),
      droppedDuplicates: strings(a?.dropped_duplicates, "dropped_duplicates"),
      missingSkills: strings(a?.missing_skills, "missing_skills"),
      mcpServers: strings(a?.mcp_servers, "mcp_servers"),
      allowedTools: strings(a?.allowed_tools, "allowed_tools"),
      enabledIn: list(a?.enabled_in, "enabled_in").map((e) => ({ id: String(e?.id ?? ""), name: String(e?.name ?? ""), mode: e?.mode === "selected" ? "selected" : "all" })),
      repositories: list(a?.repositories, "repositories").map((r) => ({
        projectId: String(r?.project_id ?? ""),
        repositoryId: String(r?.repository_id ?? ""),
        repositoryName: String(r?.repository_name ?? ""),
        unassigned: r?.unassigned === true,
        promptSet: r?.prompt_set === true,
        listVersion: String(r?.list_version ?? ""),
      })),
      usage: usage(a?.usage),
      problems: problems(a?.problems),
    })),
    bundles: list(body.bundles, "bundles").map((b) => ({
      name: String(b?.name ?? ""),
      entries: strings(b?.entries, "entries"),
      resolvedSkills: strings(b?.resolved_skills, "resolved_skills"),
      usedByAgents: strings(b?.used_by_agents, "used_by_agents"),
      usedByBundles: strings(b?.used_by_bundles, "used_by_bundles"),
    })),
    skills: list(body.skills, "skills").map((s) => ({
      name: String(s?.name ?? ""),
      description: String(s?.description ?? ""),
      fileCount: Number(s?.file_count ?? 0),
      hasExtraFiles: s?.has_extra_files === true,
      agents: strings(s?.agents, "agents"),
      bundles: strings(s?.bundles, "bundles"),
      agentCount: Number(s?.agent_count ?? 0),
      problems: problems(s?.problems),
    })),
    mcpServers: list(body.mcp_servers, "mcp_servers").map(toMcpServer),
    builtInTools: list(body.built_in_tools, "built_in_tools").map((t) => ({ name: String(t?.name ?? ""), description: String(t?.description ?? "") })),
  };
}

/** @returns {import("../domain/ports.js").SkillDetail} */
export function toSkillDetail(body) {
  const s = body?.skill;
  if (!s || typeof s.name !== "string") bad("expected {skill: {name, …}}");
  return {
    skill: {
      name: s.name,
      description: String(s.description ?? ""),
      body: String(s.body ?? ""),
      files: list(s.files, "files").map((f) => ({
        path: String(f?.path ?? ""),
        dir: f?.dir === true,
        size: Number(f?.size ?? 0),
        content: String(f?.content ?? ""),
        encoding: f?.encoding === "base64" ? "base64" : "utf8",
      })),
      agents: strings(s.agents, "agents"),
      bundles: strings(s.bundles, "bundles"),
      agentCount: Number(s.agent_count ?? 0),
    },
    versions: object(body.versions),
  };
}

/** @returns {import("../domain/ports.js").WriteResult} */
export function toWriteResult(body) {
  const c = body?.change;
  if (!c || typeof c !== "object") bad("expected {change: {…}}");
  return {
    change: {
      id: String(c.id ?? ""),
      summary: String(c.summary ?? ""),
      writes: list(c.writes, "writes").map((w) => ({ path: String(w?.path ?? ""), before: w?.before ?? null, after: w?.after ?? null, ...(w?.encoding === "base64" ? { encoding: "base64" } : {}) })),
      projects: (Array.isArray(c.projects) ? c.projects : []).map((p) => ({ id: String(p?.id ?? ""), name: String(p?.name ?? ""), before: strings(p?.before ?? [], "before"), after: strings(p?.after ?? [], "after"), enabledAgentsBefore: strings(p?.enabled_agents_before ?? [], "enabled_agents_before"), enabledAgentsAfter: strings(p?.enabled_agents_after ?? [], "enabled_agents_after") })),
    },
    versions: object(body.versions),
    probe: body.probe ? toProbeOutcome(body.probe) : null,
  };
}

/** What a save or Refresh tools probe reports: no tool list, the catalog entry has it. @returns {import("../domain/ports.js").ProbeOutcome} */
export function toProbeOutcome(p) {
  if (!p || (p.status !== "ok" && p.status !== "error")) bad("expected a probe with a status");
  return { status: p.status, error: String(p.error ?? ""), hint: String(p.hint ?? "") };
}

/** @returns {{ server: import("../domain/ports.js").CatalogMcpServer, probe: import("../domain/ports.js").ProbeOutcome }} */
export function toRefreshedTools(body) {
  if (!body?.server || typeof body.server !== "object") bad("expected {server: {…}, probe: {…}}");
  return { server: toMcpServer(body.server), probe: toProbeOutcome(body.probe) };
}

/** @returns {import("../domain/ports.js").Probe} */
export function toProbe(body) {
  const p = body?.probe;
  if (!p || (p.status !== "ok" && p.status !== "error")) bad("expected {probe: {status}}");
  return {
    status: p.status,
    tools: list(p.tools, "tools").map((t) => ({ name: String(t?.name ?? ""), description: String(t?.description ?? "") })),
    error: String(p.error ?? ""),
    hint: String(p.hint ?? ""),
  };
}

/** @param {unknown} body @returns {string[]} */
export function toServerNames(body) {
  return strings(body?.mcp_servers, "mcp_servers");
}

const STATES = ["added", "agent", "banned"];
const settingOf = (v, what) => (v === "enabled" || v === "disabled" ? v : bad(`${what} is neither enabled nor disabled`));

/** A project's MCP settings: get_project_mcp_servers, and the answer of set_project_mcp_settings. @param {any} body @returns {import("../domain/ports.js").ProjectMcpSettings} */
export function toProjectMcpSettings(body) {
  return {
    added: strings(body?.mcp_servers, "mcp_servers"),
    servers: list(body?.servers, "servers").map((s) => {
      if (!STATES.includes(s?.state)) bad("a server's state is not added, agent or banned");
      return {
        name: String(s?.name ?? ""),
        state: s.state,
        usedByAgents: strings(s?.used_by_agents, "used_by_agents"),
        tools: s?.tools === undefined || s?.tools === null ? null : list(s.tools, "tools").map((t) => ({
          name: String(t?.name ?? ""),
          default: settingOf(t?.default, "a tool's default"),
          override: t?.override === "default" ? "default" : settingOf(t?.override, "a tool's override"),
          effective: settingOf(t?.effective, "a tool's effective setting"),
        })),
      };
    }),
  };
}

/** The write body of set_project_mcp_settings. @param {string} projectId @param {{ name: string, state: string, toolOverrides?: Record<string, string> }[]} servers */
export function projectMcpSettingsBody(projectId, servers) {
  return { project_id: projectId, servers: servers.map((s) => ({ name: s.name, state: s.state, tool_overrides: s.toolOverrides ?? {} })) };
}

/** @param {unknown} body @returns {import("../domain/projectAgents.js").ProjectAgents} */
export function toProjectAgents(body) {
  const mode = body?.mode;
  if (mode !== "all" && mode !== "selected") bad("mode");
  return { mode, agents: strings(body?.agents, "agents") };
}

/** The fields every write takes. @param {import("../domain/ports.js").WriteOptions} [o] */
export function writeFields(o = {}) {
  const out = {};
  if (o.baseVersions) out.base_versions = o.baseVersions;
  if (o.force) out.force = true;
  if (o.dryRun) out.dry_run = true;
  return out;
}

const withPrevious = (previousName) => (previousName ? { previous_name: previousName } : {});

export function agentBody(input) {
  const a = input.agent;
  // `projects` is the full set of Selected projects that list the agent; left out, the lists stay as they are.
  return {
    ...withPrevious(input.previousName),
    agent: { name: a.name, description: a.description, aliases: a.aliases ?? [], entries: a.entries ?? [], mcp_servers: a.mcpServers ?? [], allowed_tools: a.allowedTools ?? [] },
    ...(input.projects === undefined ? {} : { projects: input.projects }),
  };
}

export function bundleBody(input) {
  return { ...withPrevious(input.previousName), bundle: { name: input.bundle.name, entries: input.bundle.entries ?? [] } };
}

export function skillBody(input) {
  const s = input.skill;
  return {
    ...withPrevious(input.previousName),
    skill: {
      name: s.name,
      description: s.description,
      body: s.body ?? "",
      // The server names text "utf-8"; the page says "utf8" and "base64".
      files: (s.files ?? []).map((f) => ({ path: f.path, content: f.content, encoding: f.encoding === "base64" ? "base64" : "utf-8" })),
      deleted_files: s.deletedFiles ?? [],
    },
    add_to: { agents: input.addTo?.agents ?? [], bundles: input.addTo?.bundles ?? [] },
  };
}

export function serverFields(s) {
  return { name: s.name, transport: s.transport, command: s.command ?? "", args: s.args ?? [], env: s.env ?? {}, url: s.url ?? "", headers: s.headers ?? {} };
}

export function mcpServerBody(input) {
  const disabled = input.server.disabledTools === undefined ? {} : { disabled_tools: input.server.disabledTools };
  return { ...withPrevious(input.previousName), server: { ...serverFields(input.server), ...disabled }, agents: input.agents ?? [], projects: input.projects ?? [] };
}

/** @param {unknown} body @returns {{ id: string, name: string }[]} */
export function toProjectList(body) {
  return list(/** @type {any} */ (body)?.projects, "projects").map((p) => ({ id: String(p?.id ?? ""), name: String(p?.name ?? "") }));
}
