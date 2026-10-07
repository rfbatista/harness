// What the projects module needs from the outside world. Implemented by
// infrastructure/projects-gateway.js (over /api and its SSE feed) and
// infrastructure/memory-gateway.js; both run testing/gateway-contract.js.

/**
 * @typedef {import("./project.js").Project} Project
 * @typedef {import("./project.js").Repository} Repository
 *
 * @typedef {import("./project.js").ProjectSummary} ProjectSummary
 *
 * @typedef {{ kind: "upsert", project: Project } | { kind: "deleted", id: string }} CatalogChange
 *
 * @typedef {object} ProjectGateway
 * @property {(input: { name: string, rootDir: string }) => Promise<Project>} createProject
 *           The name is trimmed; ~ in rootDir is the server user's home, and the
 *           cleaned absolute path is returned. Rejects with INVALID_INPUT without
 *           a name, PROJECT_NAME_TAKEN, or PROJECT_ROOT_INVALID unless rootDir is
 *           an existing absolute directory.
 * @property {() => Promise<ProjectSummary[]>} listProjectSummaries
 *           Every project with its counts, by name (ignoring case), then id.
 * @property {(input: { projectId: string, name?: string, rootDir?: string }) => Promise<Project>} updateProject
 *           An omitted or empty field keeps its value. The name is checked only
 *           when it changes (PROJECT_NAME_TAKEN; other capitals of its own name
 *           are no change), the root only when it changes (PROJECT_ROOT_INVALID).
 *           Rejects with PROJECT_NOT_FOUND.
 * @property {(projectId: string) => Promise<void>} deleteProject
 *           Rejects with PROJECT_NOT_FOUND, or PROJECT_HAS_RUNNING_SESSIONS whose
 *           details are { sessions: RunningSession[] } while any session is live.
 * @property {(projectId: string, path: string) => Promise<Project>} addIgnoredPath
 *           Relative to the project's directory; adding it twice keeps one.
 * @property {(projectId: string, path: string) => Promise<Project>} removeIgnoredPath
 * @property {(onChange: (change: CatalogChange) => void, onStatus?: (status: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} followCatalog
 *           Each project created, updated, re-tuned or deleted anywhere, as it
 *           happens. Counts are not on it. Returns the function that stops it.
 * @property {(seed: unknown) => ProjectSummary[]} decodeProjectsSeed
 *           Reads the projects page seed. Throws BAD_RESPONSE when malformed.
 * @property {(seed: unknown) => { project: Project, repositories: Repository[] }} decodeSettingsSeed
 *           Reads the settings page seed. Throws BAD_RESPONSE when malformed.
 * @property {(input: { projectId: string, name: string, description: string, url: string, rootDir: string }) => Promise<Repository>} addRepository
 *           Rejects with PROJECT_NOT_FOUND or INVALID_URL.
 * @property {(repositoryId: string) => Promise<void>} removeRepository
 *           Rejects with REPOSITORY_NOT_FOUND.
 * @property {(rootDir: string) => Promise<{ root: string, found: import("./project.js").FoundRepository[] }>} findRepositories
 *           The git checkouts inside a directory on the server's machine (the
 *           directory itself if it is one), and the directory as resolved
 *           (~ expanded). Rejects with INVALID_ROOT for a relative or missing one.
 * @property {(seed: unknown) => { projectId: string, projectRoot: string, repositories: Repository[] }} decodeRepositories
 * @property {(repositoryId: string, path: string, content: string) => Promise<import("./project.js").EnvFile>} saveEnvFile
 *           Creates or replaces one env file. Rejects with INVALID_PATH,
 *           ENV_FILE_TOO_LARGE or REPOSITORY_NOT_FOUND.
 * @property {(repositoryId: string, path: string) => Promise<void>} deleteEnvFile
 *           Rejects with ENV_FILE_NOT_FOUND.
 * @property {(repositoryId: string, path: string) => Promise<import("./project.js").EnvFile>} importEnvFile
 *           Saves the file as it is now in the repository's checkout. Rejects
 *           with ENV_FILE_NOT_FOUND when the checkout has none.
 * @property {(seed: unknown) => { repositoryId: string, files: import("./project.js").EnvFile[] }} decodeEnvFiles
 *           Reads the repositories page seed. Throws BAD_RESPONSE when malformed.
 */

export {};
