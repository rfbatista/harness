// CatalogGateway in memory: the fake for dev pages and unit tests. It is the
// real gateway over an in-process stand-in for the server's catalog verbs
// (memoryCatalogServer), so both speak the wire format and obey the same
// contract suite (../testing/gateway-contract.js): versions and conflicts,
// refusals, dry runs, renames that rewrite references, and Undo of the
// latest change.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { isMcpName, isSlug, placeholderNames } from "../domain/names.js";
import { catalogGateway } from "./catalog-gateway.js";

const fail = (code, message, status, details) => {
  throw new StructuredError(code, message, status, details);
};
const invalid = (field, message) => fail(Codes.INVALID_INPUT, message, 400, { field });
const clone = (v) => structuredClone(v);
const without = (list, name) => list.filter((x) => x !== name);
const swap = (list, from, to) => list.map((x) => (x === from ? to : x));

/**
 * @param {{ catalog: object, projects?: Record<string, string[]>, projectNames?: Record<string, string>, env?: Record<string, boolean> }} world
 *   catalog is the wire format of GET /api/get_agent_catalog
 */
export function memoryCatalogServer({ catalog, projects = {}, projectNames = {}, env = {} }) {
  let cat = clone(catalog);
  let projectLists = clone(projects);
  let counter = 0;
  let latest = null;
  const requests = [];

  const derive = (c) => {
    const skillNames = new Set(c.skills.map((s) => s.name));
    const bundleByName = new Map(c.bundles.map((b) => [b.name, b]));
    const expand = (entries, seen = new Set()) => {
      const out = [];
      for (const e of entries) {
        if (e.startsWith("@")) {
          const b = bundleByName.get(e.slice(1));
          if (b && !seen.has(b.name)) out.push(...expand(b.entries, new Set([...seen, b.name])));
        } else out.push(e);
      }
      return out;
    };
    for (const a of c.agents) {
      const all = expand(a.entries);
      a.resolved_skills = [...new Set(all)].filter((s) => skillNames.has(s));
      a.dropped_duplicates = all.filter((s, i) => all.indexOf(s) !== i);
      a.missing_skills = [...new Set(a.entries.filter((e) => !e.startsWith("@") && !skillNames.has(e)))];
      a.problems = [
        ...(a.description.trim() ? [] : [{ code: "NO_DESCRIPTION", message: "no description" }]),
        ...a.missing_skills.map((s) => ({ code: "MISSING_SKILL", message: `skill ${s} does not exist` })),
      ];
    }
    for (const b of c.bundles) {
      b.resolved_skills = [...new Set(expand(b.entries, new Set([b.name])))].filter((s) => skillNames.has(s));
      b.used_by_agents = c.agents.filter((a) => a.entries.includes(`@${b.name}`)).map((a) => a.name);
      b.used_by_bundles = c.bundles.filter((o) => o.entries.includes(`@${b.name}`)).map((o) => o.name);
    }
    for (const s of c.skills) {
      s.agents = c.agents.filter((a) => a.entries.includes(s.name)).map((a) => a.name);
      s.bundles = c.bundles.filter((b) => b.entries.includes(s.name)).map((b) => b.name);
      s.agent_count = c.agents.filter((a) => a.resolved_skills.includes(s.name)).length;
    }
    for (const m of c.mcp_servers) {
      const refs = placeholderNames(JSON.stringify([m.env, m.headers, m.args, m.url]));
      m.env_refs = refs.map((name) => ({ name, set: env[name] === true }));
      m.agents = c.agents.filter((a) => a.mcp_servers.includes(m.name)).map((a) => a.name);
      m.projects = Object.entries(projectLists).filter(([, names]) => names.includes(m.name)).map(([id]) => ({ id, name: projectNames[id] ?? id }));
      m.problems = m.env_refs.filter((r) => !r.set).map((r) => ({ code: "SECRET_NOT_SET", message: `${r.name} is not set` }));
    }
    return c;
  };
  cat = derive(cat);

  const agentsText = (c) =>
    JSON.stringify({ bundles: Object.fromEntries(c.bundles.map((b) => [b.name, b.entries])), agents: Object.fromEntries(c.agents.map((a) => [a.name, { description: a.description, aliases: a.aliases, skills: a.entries, mcp_servers: a.mcp_servers, allowed_tools: a.allowed_tools }])) }, null, 2);
  const serversText = (c) => JSON.stringify({ mcpServers: Object.fromEntries(c.mcp_servers.map((m) => [m.name, m.transport === "stdio" ? { command: m.command, args: m.args, env: m.env } : { type: m.transport, url: m.url, headers: m.headers }])) }, null, 2);
  const textOf = { "agents.json": agentsText, "mcp-servers.json": serversText };

  const suggest = (taken, name) => {
    for (let i = 2; ; i++) if (!taken.includes(`${name}-${i}`)) return `${name}-${i}`;
  };

  const mustSlug = (name, field = "name") => {
    if (!isSlug(name)) invalid(field, `${JSON.stringify(name)} is not a valid name: use lowercase letters, digits and hyphens`);
  };
  const mustExist = (names, known) => {
    const missing = names.filter((n) => !known.includes(n));
    if (missing.length) fail("UNKNOWN_REFERENCE", `unknown: ${missing.join(", ")}`, 400, { names: missing });
  };
  const entryNames = (c, entries) => {
    const missing = entries.filter((e) => (e.startsWith("@") ? !c.bundles.some((b) => b.name === e.slice(1)) : !c.skills.some((s) => s.name === e)));
    if (missing.length) fail("UNKNOWN_REFERENCE", `unknown: ${missing.join(", ")}`, 400, { names: missing });
  };
  const namesOf = (list) => list.map((x) => x.name);

  /** Each verb edits the draft and answers the files it touches and its summary. */
  const verbs = {
    save_catalog_agent(c, { previous_name: prev, agent }) {
      mustSlug(agent.name);
      if (!agent.description?.trim()) invalid("description", "description is required");
      const at = prev ? c.agents.findIndex((a) => a.name === prev) : -1;
      if (prev && at < 0) fail("AGENT_NOT_FOUND", `no agent ${prev}`, 404);
      if (c.agents.some((a, i) => a.name === agent.name && i !== at)) fail("NAME_TAKEN", `agent ${agent.name} exists`, 409, { suggestion: suggest(namesOf(c.agents), agent.name) });
      entryNames(c, agent.entries);
      mustExist(agent.mcp_servers, namesOf(c.mcp_servers));
      const next = { ...agent, aliases: agent.aliases ?? [], resolved_skills: [], dropped_duplicates: [], missing_skills: [], problems: [] };
      if (at < 0) c.agents.push(next);
      else c.agents[at] = next;
      return { files: ["agents.json"], summary: (d) => `Saved agents.json — ${agent.name} now has ${d.agents.find((a) => a.name === agent.name).resolved_skills.length} skills` };
    },
    delete_catalog_agent(c, { name }) {
      if (!c.agents.some((a) => a.name === name)) fail("AGENT_NOT_FOUND", `no agent ${name}`, 404);
      c.agents = c.agents.filter((a) => a.name !== name);
      return { files: ["agents.json"], summary: () => `Deleted agent ${name} from agents.json` };
    },
    save_catalog_bundle(c, { previous_name: prev, bundle }) {
      mustSlug(bundle.name);
      const at = prev ? c.bundles.findIndex((b) => b.name === prev) : -1;
      if (prev && at < 0) fail("BUNDLE_NOT_FOUND", `no bundle ${prev}`, 404);
      if (c.bundles.some((b, i) => b.name === bundle.name && i !== at)) fail("NAME_TAKEN", `bundle ${bundle.name} exists`, 409, { suggestion: suggest(namesOf(c.bundles), bundle.name) });
      if (bundle.entries.length === 0) invalid("entries", "a bundle needs at least one entry");
      const others = c.bundles.filter((_, i) => i !== at);
      entryNames({ ...c, bundles: [...others, { name: bundle.name }, ...(prev ? [{ name: prev }] : [])] }, bundle.entries);
      if (prev && prev !== bundle.name) {
        for (const o of c.bundles) o.entries = swap(o.entries, `@${prev}`, `@${bundle.name}`);
        for (const a of c.agents) a.entries = swap(a.entries, `@${prev}`, `@${bundle.name}`);
      }
      const next = { name: bundle.name, entries: bundle.entries, resolved_skills: [], used_by_agents: [], used_by_bundles: [] };
      if (at < 0) c.bundles.push(next);
      else c.bundles[at] = next;
      const includes = (from, target, seen = new Set()) => {
        if (seen.has(from)) return false;
        seen.add(from);
        const inner = (c.bundles.find((b) => b.name === from)?.entries ?? []).filter((e) => e.startsWith("@")).map((e) => e.slice(1));
        return inner.some((n) => n === target || includes(n, target, seen));
      };
      const nested = bundle.entries.filter((e) => e.startsWith("@")).map((e) => e.slice(1));
      if (nested.some((n) => n === bundle.name || includes(n, bundle.name))) fail("BUNDLE_CYCLE", "bundle cycle", 400);
      return { files: ["agents.json"], summary: () => `Saved agents.json — bundle @${bundle.name}` };
    },
    delete_catalog_bundle(c, { name }) {
      if (!c.bundles.some((b) => b.name === name)) fail("BUNDLE_NOT_FOUND", `no bundle ${name}`, 404);
      c.bundles = c.bundles.filter((b) => b.name !== name);
      for (const o of [...c.bundles, ...c.agents]) o.entries = without(o.entries, `@${name}`);
      return { files: ["agents.json"], summary: () => `Deleted bundle @${name} from agents.json` };
    },
    save_catalog_skill(c, { previous_name: prev, skill, add_to: addTo }) {
      mustSlug(skill.name);
      if (!skill.description?.trim()) invalid("description", "description is required");
      if (skill.description.length > 1024) invalid("description", "description is over 1024 characters");
      const at = prev ? c.skills.findIndex((s) => s.name === prev) : -1;
      if (prev && at < 0) fail("SKILL_NOT_FOUND", `no skill ${prev}`, 404);
      if (c.skills.some((s, i) => s.name === skill.name && i !== at)) fail("NAME_TAKEN", `skill ${skill.name} exists`, 409, { suggestion: suggest(namesOf(c.skills), skill.name) });
      mustExist(addTo.agents, namesOf(c.agents));
      mustExist(addTo.bundles, namesOf(c.bundles));
      if (prev && prev !== skill.name) for (const o of [...c.bundles, ...c.agents]) o.entries = swap(o.entries, prev, skill.name);
      for (const [list, wanted] of [[c.agents, addTo.agents], [c.bundles, addTo.bundles]]) {
        for (const o of list) {
          const has = o.entries.includes(skill.name);
          if (wanted.includes(o.name) && !has) o.entries.push(skill.name);
          if (!wanted.includes(o.name) && has) o.entries = without(o.entries, skill.name);
        }
      }
      const files = skill.files ?? [];
      const next = { name: skill.name, description: skill.description, file_count: 1 + files.length, has_extra_files: files.length > 0, agents: [], bundles: [], agent_count: 0, problems: [] };
      if (at < 0) c.skills.push(next);
      else c.skills[at] = next;
      return { files: [`skills/${skill.name}`, "agents.json"], summary: () => `Saved skills/${skill.name}/SKILL.md` };
    },
    delete_catalog_skill(c, { name }) {
      if (!c.skills.some((s) => s.name === name)) fail("SKILL_NOT_FOUND", `no skill ${name}`, 404);
      c.skills = c.skills.filter((s) => s.name !== name);
      for (const o of [...c.bundles, ...c.agents]) o.entries = without(o.entries, name);
      return { files: [`skills/${name}`, "agents.json"], summary: () => `Deleted skills/${name}` };
    },
    save_catalog_mcp_server(c, { previous_name: prev, server, agents: who, projects: inProjects }) {
      if (!isMcpName(server.name)) invalid("name", `${JSON.stringify(server.name)} is not a valid server name`);
      if (!["stdio", "http", "sse"].includes(server.transport)) invalid("transport", "transport is stdio, http or sse");
      if (server.transport === "stdio" && !server.command) invalid("command", "a local server needs a command");
      if (server.transport !== "stdio" && !server.url) invalid("url", "a remote server needs a URL");
      const at = prev ? c.mcp_servers.findIndex((m) => m.name === prev) : -1;
      if (prev && at < 0) fail("MCP_SERVER_NOT_FOUND", `no server ${prev}`, 404);
      if (c.mcp_servers.some((m, i) => m.name === server.name && i !== at)) fail("NAME_TAKEN", `server ${server.name} exists`, 409, { suggestion: suggest(namesOf(c.mcp_servers), server.name) });
      mustExist(who, namesOf(c.agents));
      mustExist(inProjects, Object.keys(projectLists));
      if (prev && prev !== server.name) {
        for (const a of c.agents) a.mcp_servers = swap(a.mcp_servers, prev, server.name);
        for (const id of Object.keys(projectLists)) projectLists[id] = swap(projectLists[id], prev, server.name);
      }
      for (const a of c.agents) {
        a.mcp_servers = without(a.mcp_servers, server.name);
        if (who.includes(a.name)) a.mcp_servers.push(server.name);
      }
      for (const id of Object.keys(projectLists)) {
        projectLists[id] = without(projectLists[id], server.name);
        if (inProjects.includes(id)) projectLists[id].push(server.name);
      }
      const next = { ...server, env_refs: [], agents: [], projects: [], problems: [] };
      if (at < 0) c.mcp_servers.push(next);
      else c.mcp_servers[at] = next;
      return { files: ["mcp-servers.json", "agents.json"], summary: () => `Saved mcp-servers.json — ${server.name}` };
    },
    delete_catalog_mcp_server(c, { name }) {
      if (!c.mcp_servers.some((m) => m.name === name)) fail("MCP_SERVER_NOT_FOUND", `no server ${name}`, 404);
      c.mcp_servers = c.mcp_servers.filter((m) => m.name !== name);
      for (const a of c.agents) a.mcp_servers = without(a.mcp_servers, name);
      for (const id of Object.keys(projectLists)) projectLists[id] = without(projectLists[id], name);
      return { files: ["mcp-servers.json", "agents.json"], summary: () => `Deleted ${name} from mcp-servers.json` };
    },
  };

  function write(verb, body) {
    const draft = clone(cat);
    const { files, summary } = verbs[verb](draft, body);
    derive(draft);
    for (const f of files) {
      if (cat.broken && cat.broken.path === f) fail("CATALOG_BROKEN", `${f}: line ${cat.broken.line}, column ${cat.broken.column}: ${cat.broken.message}`, 409);
    }
    if (!body.force && body.base_versions) {
      const changed = files.filter((f) => f in body.base_versions && body.base_versions[f] !== cat.versions[f]);
      if (changed.length) {
        fail("CATALOG_CHANGED", "a file changed on disk", 409, { current_versions: clone(cat.versions), changed: changed.map((path) => ({ path, theirs: textOf[path]?.(cat) ?? "" })) });
      }
    }
    const writes = files.map((path) => ({ path, before: textOf[path]?.(cat) ?? null, after: textOf[path]?.(draft) ?? null }));
    if (body.dry_run) return { change: { id: "", summary: summary(draft), writes }, versions: clone(cat.versions) };
    const before = { cat, projects: projectLists };
    cat = draft;
    for (const f of files) cat.versions[f] = `sha256:${f}-${++counter}`;
    const id = `change-${++counter}`;
    latest = { id, before, files, after: Object.fromEntries(files.map((f) => [f, cat.versions[f]])) };
    return { change: { id, summary: summary(cat), writes }, versions: clone(cat.versions) };
  }

  const routes = {
    "GET get_agent_catalog": () => clone(cat),
    "GET get_catalog_skill": ({ name }) => {
      const s = cat.skills.find((x) => x.name === name) ?? fail("SKILL_NOT_FOUND", `no skill ${name}`, 404);
      return { skill: { name, description: s.description, body: `# ${name}\n`, files: [], agents: s.agents, bundles: s.bundles, agent_count: s.agent_count }, versions: clone(cat.versions) };
    },
    "GET check_env_vars": ({ names }) => Object.fromEntries(String(names ?? "").split(",").filter(Boolean).map((n) => [n, env[n] === true])),
    "POST undo_catalog_change": ({ change_id: id }) => {
      if (!latest || latest.id !== id) fail("CHANGE_NOT_UNDOABLE", "only the latest change can be undone", 409);
      const moved = latest.files.filter((f) => cat.versions[f] !== latest.after[f]);
      if (moved.length) fail("CATALOG_CHANGED", "a file changed since", 409, { current_versions: clone(cat.versions), changed: moved.map((path) => ({ path, theirs: textOf[path]?.(cat) ?? "" })) });
      const files = latest.files;
      const writes = files.map((path) => ({ path, before: textOf[path]?.(cat) ?? null, after: textOf[path]?.(latest.before.cat) ?? null }));
      const versions = cat.versions;
      cat = clone(latest.before.cat);
      projectLists = clone(latest.before.projects);
      cat.versions = versions;
      for (const f of files) cat.versions[f] = `sha256:${f}-${++counter}`;
      latest = null;
      return { change: { id: "", summary: "Undid the last change", writes }, versions: clone(cat.versions) };
    },
    "POST test_catalog_mcp_server": ({ server }) => ({ probe: server.command === "missing-cmd" ? { status: "error", tools: [], error: "executable not found", hint: "Install it first." } : { status: "ok", tools: [{ name: "search", description: "Searches." }], error: "", hint: "" } }),
    "GET get_project_mcp_servers": ({ project_id: id }) => ({ mcp_servers: clone(projectLists[id] ?? fail("PROJECT_NOT_FOUND", `no project ${id}`, 404)) }),
    "POST set_project_mcp_servers": ({ project_id: id, mcp_servers: names }) => {
      if (!(id in projectLists)) fail("PROJECT_NOT_FOUND", `no project ${id}`, 404);
      mustExist(names, namesOf(cat.mcp_servers));
      projectLists[id] = [...names];
      derive(cat);
      return { mcp_servers: [...names] };
    },
  };

  return {
    /** An api client's get/post, answering in the wire format. */
    api: {
      async get(path, query = {}) {
        requests.push(["GET", path, query]);
        return (routes[`GET ${path.slice(1)}`] ?? fail(Codes.UNKNOWN, `no route GET ${path}`, 404))(query);
      },
      async post(path, body = {}) {
        requests.push(["POST", path, body]);
        const verb = path.slice(1);
        if (verbs[verb]) return write(verb, body);
        return (routes[`POST ${verb}`] ?? fail(Codes.UNKNOWN, `no route POST ${path}`, 404))(body);
      },
    },
    /** Every request: [method, path, body or query]. */
    requests,
    /** The file changed on disk behind the page's back. */
    editOnDisk(path) {
      cat.versions[path] = `sha256:${path}-${++counter}`;
    },
    /** agents.json or mcp-servers.json stops parsing. */
    breakFile(path, line, column, message) {
      cat.broken = { path, line, column, message };
    },
  };
}

/** @param {Parameters<typeof memoryCatalogServer>[0]} world */
export function memoryCatalog(world) {
  const server = memoryCatalogServer(world);
  return { gateway: catalogGateway(/** @type {any} */ (server.api)), server };
}
