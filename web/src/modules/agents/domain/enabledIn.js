// Where an agent is enabled (UX spec section 9, contract section 8). A project
// is in mode All (every catalog agent is enabled, new ones too) or Selected
// (only its list). The catalog read says, per agent, the projects it is
// enabled in (`enabledIn`: every All project and each Selected project that
// lists it), and the project list says how many projects there are. So a
// project an agent is not enabled in is a Selected project that does not list
// it, and a project's mode is All as soon as any agent is enabled there
// without being listed. No per-project read is needed once the catalog holds
// an agent; with none, the modes are read once (`known`).
//
// The editor's Projects section ticks the Selected projects that list the
// agent. `projects` of the save is that full set, and is sent only when the
// person touched the section and it differs from what the catalog says.

/**
 * @typedef {import("./ports.js").Catalog} Catalog
 * @typedef {import("./ports.js").CatalogAgent} CatalogAgent
 * @typedef {{ id: string, name: string }} ProjectRef
 * @typedef {"all" | "selected"} Mode
 */

const enc = encodeURIComponent;
const byName = (a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id);
const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/** Where a project's own settings are. @param {string} id */
export const projectSettingsHref = (id) => `/projects/${enc(id)}/settings`;

/**
 * Every project there is: the project list, plus any project an agent is
 * enabled in that the list did not carry (it is read at another moment). The
 * list's name wins.
 * @param {Pick<Catalog, "agents">} catalog
 * @param {ProjectRef[]} projects
 * @returns {ProjectRef[]}
 */
export function allProjects(catalog, projects) {
  const out = new Map(projects.map((p) => [p.id, { id: p.id, name: p.name }]));
  for (const a of catalog.agents) for (const e of a.enabledIn ?? []) if (!out.has(e.id)) out.set(e.id, { id: e.id, name: e.name });
  return [...out.values()].sort(byName);
}

/**
 * Each project's mode. All when any agent is enabled there as an All project;
 * otherwise Selected (an agent absent from its list proves the list is what
 * counts). With no agent in the catalog nothing proves it, so `known` (read
 * from the server) says, and a project it does not name is taken as Selected.
 * @param {Pick<Catalog, "agents">} catalog
 * @param {ProjectRef[]} projects
 * @param {Record<string, Mode>} [known]
 * @returns {Record<string, Mode>}
 */
export function projectModes(catalog, projects, known = {}) {
  const all = new Set(catalog.agents.flatMap((a) => (a.enabledIn ?? []).filter((e) => e.mode === "all").map((e) => e.id)));
  return Object.fromEntries(allProjects(catalog, projects).map((p) => [p.id, all.has(p.id) ? "all" : known[p.id] ?? "selected"]));
}

/** The projects whose mode the catalog cannot prove: read only when the catalog holds no agent. @param {Pick<Catalog, "agents">} catalog @param {ProjectRef[]} projects */
export function projectsToRead(catalog, projects) {
  return catalog.agents.length === 0 ? allProjects(catalog, projects).map((p) => p.id) : [];
}

/**
 * The Enabled in section of an agent's detail.
 * @param {Pick<Catalog, "agents">} catalog
 * @param {CatalogAgent} agent
 * @param {ProjectRef[]} projects
 */
export function enabledInView(catalog, agent, projects) {
  const everyone = allProjects(catalog, projects);
  const enabledIn = agent.enabledIn ?? [];
  const on = new Set(enabledIn.map((e) => e.id));
  const rows = [...enabledIn].sort(byName).map((e) => {
    const auto = e.mode === "all";
    return {
      id: e.id,
      name: e.name,
      href: projectSettingsHref(e.id),
      mode: e.mode,
      auto,
      badge: auto ? "All agents" : "Selected agents",
      tone: auto ? "" : "signal",
      note: auto ? "Enabled automatically: every agent is enabled here, this one included. Change it in the project’s settings." : "On the project’s list",
    };
  });
  const missing = everyone.filter((p) => !on.has(p.id));
  const notListed = missing.map((p, i) => ({ id: p.id, name: p.name, href: projectSettingsHref(p.id), sep: i < missing.length - 2 ? ", " : i === missing.length - 2 ? " and " : "" }));
  const total = everyone.length;
  return {
    heading: `Enabled in · ${rows.length} of ${total} projects`,
    rows,
    hasRows: rows.length > 0,
    noProjects: total === 0,
    notListed,
    hasNotListed: notListed.length > 0,
    /** Enabled nowhere, with projects to enable it in. */
    nowhere: rows.length === 0 && total > 0,
    nowhereText: `No session can start as ${agent.name}. Tick it in a Selected-agents project, or switch a project to All agents.`,
  };
}

/** The Selected projects that list the agent now, by the catalog. @param {Pick<Catalog, "agents">} catalog @param {string} name  "" for a new agent */
export function listedIn(catalog, name) {
  const a = name ? catalog.agents.find((x) => x.name === name) : undefined;
  return a ? (a.enabledIn ?? []).filter((e) => e.mode === "selected").map((e) => e.id) : [];
}

/** The ids ticked in the section: what the person set, or else what the catalog says. */
const ticked = (catalog, draft) => draft.projects ?? listedIn(catalog, draft.previousName);

/**
 * The Projects section of the editor: a row per project, the Selected ones
 * ticked when they list the agent, the All ones fixed and ticked.
 * @param {Pick<Catalog, "agents">} catalog
 * @param {{ previousName: string, projects: string[] | null }} draft
 * @param {ProjectRef[]} projects
 * @param {Record<string, Mode>} [known]
 */
export function projectChoices(catalog, draft, projects, known = {}) {
  const modes = projectModes(catalog, projects, known);
  const on = new Set(ticked(catalog, draft));
  const rows = allProjects(catalog, projects).map((p) => {
    const fixed = modes[p.id] === "all";
    return {
      id: p.id,
      name: p.name,
      fixed,
      checked: fixed || on.has(p.id),
      badge: fixed ? "All agents" : "Selected agents",
      tone: fixed ? "" : "signal",
      href: projectSettingsHref(p.id),
      note: fixed ? "Enabled automatically." : "",
      linkLabel: `Open ${p.name}’s settings`,
      inputId: `agent-project-${p.id}`,
    };
  });
  const all = rows.filter((r) => r.fixed).length;
  const sel = rows.filter((r) => !r.fixed && r.checked).length;
  const n = all + sel;
  return {
    rows,
    count: n,
    total: rows.length,
    summary: rows.length === 0 ? "No projects yet" : `Enabled in ${n} of ${rows.length} projects · ${all} by All agents, ${plural(sel, "Selected-agents list", "Selected-agents lists")}`,
    tocCount: rows.length === 0 ? "" : String(n),
  };
}

/** The ids ticked after the person flips `id`. @param {Pick<Catalog, "agents">} catalog @param {{ previousName: string, projects: string[] | null }} draft @param {string} id */
export function toggleProject(catalog, draft, id) {
  const now = ticked(catalog, draft);
  return now.includes(id) ? now.filter((x) => x !== id) : [...now, id];
}

const sameSet = (a, b) => a.length === b.length && a.every((x) => b.includes(x));

/** Whether the section differs from the catalog: an untouched one never does. @param {Pick<Catalog, "agents">} catalog @param {{ previousName: string, projects: string[] | null }} draft */
export function projectsChanged(catalog, draft) {
  return draft.projects !== null && !sameSet(draft.projects, listedIn(catalog, draft.previousName));
}

/**
 * `projects` of the save: the full set of Selected projects that list the
 * agent, or undefined (leave the lists as they are) when the section was
 * untouched or put back as it was.
 * @param {Pick<Catalog, "agents">} catalog
 * @param {{ previousName: string, projects: string[] | null }} draft
 * @returns {string[] | undefined}
 */
export function projectsToSend(catalog, draft) {
  return projectsChanged(catalog, draft) ? [.../** @type {string[]} */ (draft.projects)] : undefined;
}

/**
 * Review change: the projects whose enabled-agents list the change sets, as a
 * diff of the list. A project whose MCP servers alone change is left out.
 * @param {import("./ports.js").WriteResult["change"]["projects"]} entries
 */
export function projectChangeFiles(entries) {
  return entries
    .filter((p) => (p.enabledAgentsBefore ?? []).join("\n") !== (p.enabledAgentsAfter ?? []).join("\n"))
    .map((p) => {
      const before = p.enabledAgentsBefore ?? [];
      const after = p.enabledAgentsAfter ?? [];
      const lines = [
        ...before.filter((n) => !after.includes(n)).map((n) => ({ kind: "del", mark: "−", text: n })),
        ...after.filter((n) => !before.includes(n)).map((n) => ({ kind: "add", mark: "+", text: n })),
        ...after.filter((n) => before.includes(n)).map((n) => ({ kind: "same", mark: " ", text: n })),
      ].map((l, key) => ({ ...l, key }));
      const added = lines.filter((l) => l.kind === "add").length;
      const removed = lines.filter((l) => l.kind === "del").length;
      return {
        path: `${p.name} · enabled agents`,
        id: p.id,
        label: `+${added} −${removed}`,
        lines,
        empty: after.length === 0,
        emptyNote: after.length === 0 ? "Its list is left empty: no agent can be picked there." : "",
      };
    });
}
