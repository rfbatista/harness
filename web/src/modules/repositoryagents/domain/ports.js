// What the repository agents views need from the outside world: a
// repository's assigned agents and repository prompts (ADR 0061, contract
// "Harness server ↔ Web UI — repository agents and repository prompts").
// Implemented by infrastructure/repository-agents-gateway.js (over /api and
// its SSE feed) and infrastructure/memory-gateway.js; both run
// testing/gateway-contract.js.

/**
 * @typedef {{ set: boolean, summary: string, length: number, version: string }} PromptState
 *
 * @typedef {object} AgentRow
 * @property {string} name
 * @property {string} description
 * @property {"assigned"|"unassigned"|"not_enabled"} state
 * @property {boolean} unassigned  the list itself, whatever the state
 * @property {PromptState} prompt
 * @property {number} runningSessions
 * @property {string} previousName  "" unless renamed in the last 7 days
 *
 * @typedef {object} RepositoryAgents
 * @property {{ id: string, name: string }} repository
 * @property {{ id: string, name: string, agentsMode: "all"|"selected", enabledCount: number, catalogCount: number }} project
 * @property {string} listVersion
 * @property {AgentRow[]} agents
 * @property {number} [runningSessions]  only on the answer to unassignAgent
 *
 * @typedef {{ agent: string, repositoryId: string, content: string, version: string, limit: number, unassigned: boolean }} RepositoryPrompt
 * @typedef {{ agentPrompt: string, repositorySection: string, heading: string }} PromptPreview
 * @typedef {{ kind: "changed", projectId: string, repositoryId: string, agents: string[] }} AgentsChange
 *
 * @typedef {object} RepositoryAgentsGateway
 * @property {(repositoryId: string) => Promise<RepositoryAgents>} getRepositoryAgents
 *           Rejects with REPOSITORY_NOT_FOUND.
 * @property {(input: { repositoryId: string, agent: string, listVersion: string }) => Promise<RepositoryAgents>} unassignAgent
 *           Accepted and a no-op when already unassigned. Rejects with
 *           REPOSITORY_AGENTS_CHANGED (details.current: RepositoryAgents),
 *           AGENT_NOT_FOUND or REPOSITORY_NOT_FOUND.
 * @property {(input: { repositoryId: string, agent: string, listVersion: string }) => Promise<RepositoryAgents>} assignAgentAgain
 * @property {(input: { repositoryId: string, listVersion: string }) => Promise<RepositoryAgents>} assignAllAgentsAgain
 * @property {(repositoryId: string, agent: string) => Promise<RepositoryPrompt>} getRepositoryPrompt
 * @property {(input: { repositoryId: string, agent: string, content: string, version: string }) => Promise<RepositoryPrompt>} saveRepositoryPrompt
 *           Blank content clears it (version ""). Rejects with
 *           REPOSITORY_PROMPT_CHANGED (details.current: RepositoryPrompt) or
 *           REPOSITORY_PROMPT_TOO_LONG (details: { limit, length }).
 * @property {(input: { repositoryId: string, agent: string, content: string }) => Promise<PromptPreview>} previewRepositoryPrompt
 *           The server's own assembly; stores nothing; content may be unsaved.
 * @property {(onChange: (change: AgentsChange) => void, onStatus?: (status: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} followChanges
 *           repository_agents_changed, as it happens. A RESYNCED status means
 *           changes were missed: re-read. Returns the function that stops it.
 * @property {(body: unknown) => RepositoryAgents} decodeRepositoryAgents
 * @property {(body: unknown) => RepositoryPrompt} decodeRepositoryPrompt
 */
export {};
