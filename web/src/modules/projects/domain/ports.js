// What the projects module needs from the outside world. Implemented by
// infrastructure/projects-gateway.js (over /api) and
// infrastructure/memory-gateway.js; both run testing/gateway-contract.js.

/**
 * @typedef {import("./project.js").Project} Project
 * @typedef {import("./project.js").Repository} Repository
 *
 * @typedef {object} ProjectGateway
 * @property {(input: { name: string, rootDir: string }) => Promise<Project>} createProject
 *           Rejects with INVALID_ROOT without a root directory.
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
