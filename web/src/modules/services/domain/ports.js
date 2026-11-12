// What the services module needs from the outside world. Implemented by
// infrastructure/services-gateway.js over the harness API (contract: Harness
// server ↔ Web UI — services).

/**
 * @typedef {object} ServicesSettings
 * @property {"docker" | "podman"} container_runtime
 * @property {boolean} available  whether the runtime's command line is found on the server
 */

/**
 * @typedef {object} Service  the API's service, mapped (infrastructure/dto.js); `run` keeps the API's field names (worktree_key, overrides: [{name, by}]), see the contract's Run
 * @property {string} id
 * @property {string} projectId
 * @property {string} name
 * @property {"command" | "image"} kind
 * @property {string} repositoryId
 * @property {boolean} exportRepositoryEnv
 * @property {string} env  as saved; shown only in the service form
 * @property {{ line: string, subdir: string, ports: number[] } | null} command
 * @property {{ ref: string, args: string[], ports: { host: number, container: number, protocol: "tcp" | "udp" }[] } | null} image
 * @property {"stopped" | "starting" | "running" | "exited" | "failed"} status
 * @property {object | null} run
 * @property {{ kind: "person" | "session", sessionId: string, taskId: string, agent: string }} createdBy
 * @property {number} pendingRequests  service requests waiting on the person
 */

/**
 * @typedef {import("../../runs/domain/run.js").RunWorktree} RunWorktree  a run worktree of a command service (list_service_run_worktrees): key "main" or "session:<id>"
 *
 * @typedef {object} ServiceRequest  an agent's request to create, change, start, restart or stop a service, for the person to answer (contract section 13)
 * @property {string} id
 * @property {string} projectId
 * @property {string} serviceId  "" for a create until approved
 * @property {string} serviceName
 * @property {"create" | "update" | "start" | "restart" | "stop"} action
 * @property {string} sessionId
 * @property {string} taskId
 * @property {string} agent
 * @property {string} reason  the agent's own words
 * @property {string} summary  one readable line
 * @property {object | null} proposed  create: the whole definition; update: the definition after (the API's ServiceInput)
 * @property {{ field: string, before: string, after: string }[]} changes  update only
 * @property {{ name: string, value: string, change: "set" | "unset", previousSet: boolean }[]} env  variables set or unset; run overrides for start and restart. `value` is "" on a feed event
 * @property {string} worktree  start and restart: main or own
 * @property {"pending" | "approved" | "denied" | "expired" | "done" | "failed"} state
 * @property {string} note  the person's
 * @property {string} resultError  failed: why the approved action failed
 * @property {string} requestedAt
 * @property {string} answeredAt
 *
 * @typedef {object} BranchList  a list_service_branches answer
 * @property {{ name: string, remote: boolean, sha: string, subject: string, committedAt: string }[]} branches  local first, then origin/*, newest commit first; a remote branch's name has no origin/
 * @property {boolean} truncated  there were more than the 200 returned
 *
 * @typedef {object} RunVariables  a get_run_variables answer
 * @property {string} runId  "" for a service that never ran
 * @property {{ name: string, value: string, source: "repository_env" | "service" | "override", by: string }[]} variables  sorted by name; values are the person's to see, never stored or logged
 */

/**
 * @typedef {object} ServiceList
 * @property {Service[]} services
 * @property {number} seq  the services feed's sequence number when it was read
 * @property {{ name: "docker" | "podman", available: boolean }} runtime
 * @property {"on" | "off" | "unavailable"} sandbox
 */

/**
 * @typedef {object} ServiceEvent
 * @property {number} seq
 * @property {string} kind  service_changed | service_deleted | service_request
 * @property {Service | null} service
 * @property {ServiceRequest | null} request  service_request: the request without values
 * @property {string} serviceId
 * @property {number | null} runningCount  null for an event that carries none (service_request)
 */

/**
 * @typedef {object} LogRead  a read_service_log answer
 * @property {string} text
 * @property {number} offset  where `text` starts, in bytes since the run began
 * @property {number} next_offset  where it ends: the log feed follows from here
 * @property {number} first_offset  the first byte the server still keeps
 * @property {string} run_id
 */

/**
 * @typedef {object} ServicesGateway
 * @property {(seed: unknown) => { projectId: string, list: ServiceList | null }} decodeSeed
 * @property {(projectId: string) => Promise<ServiceList>} listServices
 * @property {(projectId: string, input: object) => Promise<Service>} createService
 *           `input` is the API's ServiceInput. Rejects with INVALID_INPUT naming the field, SERVICE_NAME_TAKEN or REPOSITORY_NOT_FOUND.
 * @property {(serviceId: string, input: object) => Promise<{ service: Service, restartNeeded: boolean }>} updateService
 * @property {(serviceId: string) => Promise<void>} deleteService  Rejects with SERVICE_RUNNING while it runs.
 * @property {(serviceId: string, worktree?: string) => Promise<{ service: Service, alreadyRunning: boolean }>} startService
 *           `worktree` is a run worktree key; sent only when given (the server reads none as the main checkout).
 *           Rejects with SANDBOX_UNAVAILABLE or RUNTIME_UNAVAILABLE when nothing was started, WORKTREE_NOT_AVAILABLE when the worktree is gone.
 * @property {(serviceId: string) => Promise<RunWorktree[]>} listRunWorktrees  the main checkout first, then session worktrees; [] for an image service or one with no repository
 * @property {(projectId: string, filter?: { state?: string, serviceId?: string }) => Promise<ServiceRequest[]>} listRequests  newest first, with variable values: read when a card opens, never kept
 * @property {(requestId: string, decision: "approve" | "deny", note?: string) => Promise<ServiceRequest>} answerRequest  rejects with REQUEST_NOT_PENDING (409) when it is not pending
 * @property {(serviceId: string, query?: string) => Promise<BranchList>} listBranches  the query filters by a case-insensitive part of the name; sent only when not empty
 * @property {(serviceId: string) => Promise<RunVariables>} getRunVariables
 * @property {(serviceId: string) => Promise<Service>} stopService
 * @property {(serviceId: string) => Promise<Service>} restartService
 * @property {(serviceId: string, offset?: number) => Promise<LogRead>} readLog
 *           The last 64 KB of the current or last run's log, or from `offset`.
 * @property {(serviceId: string, from: number, onEvent: (event: { kind: "chunk" | "reset", run_id: string, offset: number, text: string }) => void,
 *   onStatus?: (status: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} followLog
 *           Follows the log feed from byte offset `from`; returns the function that stops following.
 * @property {(projectId: string, onEvent: (event: ServiceEvent) => void,
 *   onStatus?: (status: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} follow
 *           Follows the project's services feed; returns the function that stops following.
 * @property {() => Promise<ServicesSettings>} getSettings
 * @property {(runtime: string) => Promise<ServicesSettings>} setSettings
 *           Rejects with INVALID_INPUT for any value but docker or podman.
 */

export {};
