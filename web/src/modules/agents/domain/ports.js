// What the agents module needs from the outside world: the agent catalog
// (contract: Harness server ↔ Web UI — the agent catalog). Implemented by
// infrastructure/catalog-gateway.js (over /api) and
// infrastructure/memory-gateway.js (dev and unit tests). Both run
// testing/gateway-contract.js.

/**
 * @typedef {{ code: string, message: string }} Problem
 *
 * @typedef {object} CatalogAgent
 * @property {string} name  its key
 * @property {string} description
 * @property {string[]} aliases
 * @property {string[]} entries  skill names and "@bundle" entries
 * @property {string[]} resolvedSkills  after bundles, duplicates dropped
 * @property {string[]} droppedDuplicates
 * @property {string[]} missingSkills
 * @property {string[]} mcpServers
 * @property {string[]} allowedTools  empty: Claude's default tool set
 * @property {{ id: string, name: string, mode: "all" | "selected" }[]} enabledIn  every project the agent is enabled in: each All project, and each Selected project that lists it
 * @property {Problem[]} problems
 *
 * @typedef {object} CatalogBundle
 * @property {string} name
 * @property {string[]} entries
 * @property {string[]} resolvedSkills
 * @property {string[]} usedByAgents
 * @property {string[]} usedByBundles
 *
 * @typedef {object} CatalogSkill
 * @property {string} name
 * @property {string} description
 * @property {number} fileCount
 * @property {boolean} hasExtraFiles
 * @property {string[]} agents  naming it directly
 * @property {string[]} bundles  holding it
 * @property {number} agentCount  every agent it reaches
 * @property {Problem[]} problems
 *
 * @typedef {object} CatalogMcpServer
 * @property {string} name
 * @property {string} transport  stdio, http or sse
 * @property {string} command
 * @property {string[]} args
 * @property {Record<string, string>} env
 * @property {string} url
 * @property {Record<string, string>} headers
 * @property {{ name: string, set: boolean }[]} envRefs  never a value
 * @property {string[]} agents
 * @property {{ id: string, name: string }[]} projects
 * @property {Problem[]} problems
 *
 * @typedef {object} Catalog
 * @property {string} root  the catalog folder on the server
 * @property {Record<string, string>} versions  per file; sent back as baseVersions
 * @property {{ path: string, line: number, column: number, message: string } | null} broken
 * @property {CatalogAgent[]} agents
 * @property {CatalogBundle[]} bundles
 * @property {CatalogSkill[]} skills
 * @property {CatalogMcpServer[]} mcpServers
 * @property {{ name: string, description: string }[]} builtInTools
 *
 * @typedef {object} SkillDetail
 * @property {{ name: string, description: string, body: string, files: { path: string, dir: boolean, size: number, content: string, encoding: "utf8"|"base64" }[], agents: string[], bundles: string[], agentCount: number }} skill
 * @property {Record<string, string>} versions
 *
 * @typedef {object} WriteOptions
 * @property {Record<string, string>} [baseVersions]  the versions of the files the write changes, as the editor read them
 * @property {boolean} [force]  overwrite an outside change
 * @property {boolean} [dryRun]  write nothing; answer what would be written
 *
 * @typedef {object} WriteResult
 * @property {{ id: string, summary: string, writes: { path: string, before: string | null, after: string | null, encoding?: "base64" }[], projects: { id: string, name: string, before: string[], after: string[], enabledAgentsBefore: string[], enabledAgentsAfter: string[] }[] }} change
 *           id is "" for a dry run; before is null for a new file, after for a deleted one;
 *           projects are the project lists the change sets: MCP servers (before, after) and enabled agents (enabledAgentsBefore, enabledAgentsAfter)
 * @property {Record<string, string>} versions
 *
 * @typedef {object} Probe
 * @property {"ok"|"error"} status
 * @property {{ name: string, description: string }[]} tools
 * @property {string} error
 * @property {string} hint
 *
 * @typedef {object} CatalogGateway
 * @property {(seed: unknown) => Catalog} decodeCatalog  the page's first paint, as the server seeded it
 * @property {(handlers: { onEvent: (data: unknown) => void, onStatus?: (status: import("../../../shared/domain/feed.js").FeedStatus) => void }) => () => void} follow  the page feed (/api/project_events): every message, whatever it is, and the feed's status; returns the function that stops following
 * @property {() => Promise<Catalog>} getCatalog
 * @property {(name: string) => Promise<SkillDetail>} getSkill  rejects SKILL_NOT_FOUND
 * @property {(names: string[]) => Promise<Record<string, boolean>>} checkEnvVars  whether each is set on the server; never a value
 * @property {(input: { previousName?: string, agent: { name: string, description: string, aliases: string[], entries: string[], mcpServers: string[], allowedTools: string[] }, projects?: string[] }, options?: WriteOptions) => Promise<WriteResult>} saveAgent
 * @property {(name: string, options?: WriteOptions) => Promise<WriteResult>} deleteAgent
 * @property {(input: { previousName?: string, bundle: { name: string, entries: string[] } }, options?: WriteOptions) => Promise<WriteResult>} saveBundle
 * @property {(name: string, options?: WriteOptions) => Promise<WriteResult>} deleteBundle
 * @property {(input: { previousName?: string, skill: { name: string, description: string, body: string, files: { path: string, content: string, encoding?: string }[], deletedFiles: string[] }, addTo: { agents: string[], bundles: string[] } }, options?: WriteOptions) => Promise<WriteResult>} saveSkill
 * @property {(name: string, options?: WriteOptions) => Promise<WriteResult>} deleteSkill
 * @property {(input: { previousName?: string, server: { name: string, transport: string, command?: string, args?: string[], env?: Record<string, string>, url?: string, headers?: Record<string, string> }, agents: string[], projects: string[] }, options?: WriteOptions) => Promise<WriteResult>} saveMcpServer
 * @property {(name: string, options?: WriteOptions) => Promise<WriteResult>} deleteMcpServer
 * @property {(changeId: string) => Promise<WriteResult>} undoChange  rejects CHANGE_NOT_UNDOABLE or CATALOG_CHANGED
 * @property {(server: object) => Promise<Probe>} testMcpServer  an unsaved server; stores nothing
 * @property {() => Promise<{ id: string, name: string }[]>} listProjects  every project, for Who gets it
 * @property {(projectId: string) => Promise<string[]>} getProjectMcpServers
 * @property {(projectId: string, names: string[]) => Promise<string[]>} setProjectMcpServers
 * @property {(projectId: string) => Promise<import("./projectAgents.js").ProjectAgents>} getProjectAgents  the project's enabled agents: mode all (agents empty) or selected
 * @property {(projectId: string, enabled: import("./projectAgents.js").ProjectAgents) => Promise<import("./projectAgents.js").ProjectAgents>} setProjectAgents  answers no change id; rejects UNKNOWN_REFERENCE naming the agents
 *
 * Writes reject with INVALID_INPUT, NAME_TAKEN, UNKNOWN_REFERENCE,
 * BUNDLE_CYCLE, CATALOG_CHANGED, CATALOG_BROKEN or a *_NOT_FOUND; their
 * extra fields are on the StructuredError's details (domain/refusals.js
 * reads them). Every decode throws BAD_RESPONSE when its data is malformed.
 */

export {};
