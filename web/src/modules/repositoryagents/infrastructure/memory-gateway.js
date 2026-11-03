// RepositoryAgentsGateway in memory, for tests and web/dev pages. It obeys the
// same contract as the real gateway (../testing/gateway-contract.js): stored
// versions that change on every write, no-op repeats, stale refusals that
// carry the current state, blank saves that clear, and one event per write.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { toAgentsChange, toPreview, toRepositoryAgents, toRepositoryPrompt } from "./dto.js";

const LIMIT = 8000;
const SUMMARY_MAX = 120;

const fail = (code, message, status, details) => {
  throw new StructuredError(code, message, status, details);
};

/**
 * @typedef {object} World
 * @property {{ name: string, description?: string, prompt?: string }[]} catalog
 * @property {{ id: string, name: string, agentsMode?: "all"|"selected", enabled?: string[] }[]} projects
 * @property {{ id: string, name: string, projectId: string }[]} repositories
 * @property {Record<string, string[]>} [unassigned]  repository id → unassigned agents
 * @property {Record<string, string>} [prompts]       "repository id/agent" → content
 * @property {Record<string, number>} [running]       "repository id/agent" → running sessions
 * @property {Record<string, string>} [renamed]       agent → its previous name
 */

/** @param {World} world */
export function memoryRepositoryAgents({ catalog, projects, repositories, unassigned = {}, prompts = {}, running = {}, renamed = {} }) {
  const lists = new Map(repositories.map((r) => [r.id, { names: [...(unassigned[r.id] ?? [])], version: 1 }]));
  /** "repository id/agent" → { content, version } */
  const promptStore = new Map(Object.entries(prompts).map(([k, content]) => [k, { content, version: 1 }]));
  let clock = 1;
  const followers = new Set();

  const repoOf = (id) => repositories.find((r) => r.id === id) ?? fail(Codes.REPOSITORY_NOT_FOUND, "repository not found", 404);
  const projectOf = (repo) => projects.find((p) => p.id === repo.projectId);
  const agentOf = (name) => catalog.find((a) => a.name === name) ?? fail("AGENT_NOT_FOUND", `agent ${name} not found`, 404);
  const enabledIn = (project, name) => (project.agentsMode ?? "all") === "all" || (project.enabled ?? []).includes(name);
  const listVersion = (repoId) => `l${lists.get(repoId).version}`;
  const promptVersion = (key) => (promptStore.has(key) ? `v${promptStore.get(key).version}` : "");

  function tabDTO(repo) {
    const project = projectOf(repo);
    const list = lists.get(repo.id).names;
    const rows = catalog.filter((a) => enabledIn(project, a.name) || list.includes(a.name));
    return {
      repository: { id: repo.id, name: repo.name },
      project: {
        id: project.id,
        name: project.name,
        agents_mode: project.agentsMode ?? "all",
        enabled_count: catalog.filter((a) => enabledIn(project, a.name)).length,
        catalog_count: catalog.length,
      },
      list_version: listVersion(repo.id),
      agents: rows.map((a) => {
        const key = `${repo.id}/${a.name}`;
        const content = promptStore.get(key)?.content ?? "";
        const gone = list.includes(a.name);
        return {
          name: a.name,
          description: a.description ?? "",
          state: !enabledIn(project, a.name) ? "not_enabled" : gone ? "unassigned" : "assigned",
          unassigned: gone,
          prompt: { set: content !== "", summary: content.split("\n")[0].slice(0, SUMMARY_MAX), length: content.length, version: promptVersion(key) },
          running_sessions: running[key] ?? 0,
          ...(renamed[a.name] ? { previous_name: renamed[a.name] } : {}),
        };
      }),
    };
  }
  const tab = (repo, extra = {}) => toRepositoryAgents({ ...tabDTO(repo), ...extra });

  function promptDTO(repo, agent) {
    const key = `${repo.id}/${agent}`;
    return {
      agent,
      repository_id: repo.id,
      content: promptStore.get(key)?.content ?? "",
      version: promptVersion(key),
      limit: LIMIT,
      unassigned: lists.get(repo.id).names.includes(agent),
    };
  }

  function announce(repo, agents) {
    const change = toAgentsChange({ repository_agents_changed: { project_id: repo.projectId, repository_id: repo.id, agents } });
    queueMicrotask(() => {
      for (const f of [...followers]) f.onChange(change);
    });
  }

  /** The repository, after the stale check on its list version. */
  function checked(repositoryId, listVersionSeen) {
    const repo = repoOf(repositoryId);
    if (listVersionSeen !== listVersion(repo.id)) {
      fail(Codes.REPOSITORY_AGENTS_CHANGED, "the agents of this repository changed", 409, { current: tab(repo) });
    }
    return repo;
  }

  function touch(repo) {
    lists.get(repo.id).version = ++clock;
  }

  /** @type {import("../domain/ports.js").RepositoryAgentsGateway} */
  const gateway = {
    decodeRepositoryAgents: toRepositoryAgents,
    decodeRepositoryPrompt: toRepositoryPrompt,

    async getRepositoryAgents(repositoryId) {
      return tab(repoOf(repositoryId));
    },

    async unassignAgent({ repositoryId, agent, listVersion: seen }) {
      const repo = checked(repositoryId, seen);
      agentOf(agent);
      const list = lists.get(repo.id);
      if (!list.names.includes(agent)) {
        list.names.push(agent);
        touch(repo);
        announce(repo, [agent]);
      }
      return tab(repo, { running_sessions: running[`${repo.id}/${agent}`] ?? 0 });
    },

    async assignAgentAgain({ repositoryId, agent, listVersion: seen }) {
      const repo = checked(repositoryId, seen);
      agentOf(agent);
      const list = lists.get(repo.id);
      if (list.names.includes(agent)) {
        list.names = list.names.filter((n) => n !== agent);
        touch(repo);
        announce(repo, [agent]);
      }
      return tab(repo);
    },

    async assignAllAgentsAgain({ repositoryId, listVersion: seen }) {
      const repo = checked(repositoryId, seen);
      const list = lists.get(repo.id);
      if (list.names.length > 0) {
        const was = list.names;
        list.names = [];
        touch(repo);
        announce(repo, was);
      }
      return tab(repo);
    },

    async getRepositoryPrompt(repositoryId, agent) {
      const repo = repoOf(repositoryId);
      agentOf(agent);
      return toRepositoryPrompt(promptDTO(repo, agent));
    },

    async saveRepositoryPrompt({ repositoryId, agent, content, version }) {
      const repo = repoOf(repositoryId);
      agentOf(agent);
      const key = `${repo.id}/${agent}`;
      if (version !== promptVersion(key)) {
        fail(Codes.REPOSITORY_PROMPT_CHANGED, "this prompt was saved by someone else", 409, { current: toRepositoryPrompt(promptDTO(repo, agent)) });
      }
      if (content.trim() === "") promptStore.delete(key);
      else if (content.length > LIMIT) {
        fail(Codes.REPOSITORY_PROMPT_TOO_LONG, `a repository prompt holds at most ${LIMIT} characters`, 400, { limit: LIMIT, length: content.length });
      } else promptStore.set(key, { content, version: ++clock });
      announce(repo, [agent]);
      return toRepositoryPrompt(promptDTO(repo, agent));
    },

    async previewRepositoryPrompt({ repositoryId, agent, content }) {
      const repo = repoOf(repositoryId);
      const heading = `## Repository prompt: ${repo.name}`;
      return toPreview({
        agent_prompt: agentOf(agent).prompt ?? `You are ${agent}.`,
        repository_section: content.trim() === "" ? "" : `${heading}\n\n${content.trim()}`,
        heading,
      });
    },

    followChanges(onChange, onStatus = () => {}) {
      const follower = { onChange, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  return {
    gateway,
    /** For the HTTP stub: the wire answers. */
    wire: { tab: (repo, extra) => ({ ...tabDTO(repo), ...extra }), prompt: promptDTO, repo: repoOf },
    /** The page feed drops and comes back: followers must re-read. */
    resync() {
      for (const f of [...followers]) f.onStatus(FeedStatus.RESYNCED);
    },
    followers,
  };
}
