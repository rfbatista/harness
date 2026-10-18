// What the Token savers page's Repository indexes section needs from the
// outside world. Implemented by infrastructure/repository-indexes-gateway.js
// (over /api).

/**
 * @typedef {import("./repository-index.js").RepositoryIndex} RepositoryIndex
 *
 * @typedef {object} RepositoryIndexGateway
 * @property {(seed: unknown) => RepositoryIndex[]} decodeSeed
 *           The indexes, as the server seeded them.
 * @property {() => Promise<RepositoryIndex[]>} list
 * @property {(repositoryId: string) => Promise<RepositoryIndex>} rebuild
 *           Starts a build in the background. Rejects with
 *           TOKEN_SAVER_NOT_INSTALLED, REPOSITORY_NOT_FOUND or INVALID_ROOT.
 */

export {};
