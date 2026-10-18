// What the tokensavers module needs from the outside world: activations,
// installations, repository indexes and the caveman intensity. Implemented
// by infrastructure/tokensavers-gateway.js (over /api) and
// infrastructure/memory-gateway.js (tests). Both run
// testing/gateway-contract.js.

/**
 * @typedef {"project" | "repository" | "task"} ActivationLevel
 * @typedef {"inherit" | "on" | "off"} ActivationValue
 * @typedef {"lite" | "full" | "ultra"} CavemanIntensity
 * @typedef {import("./installation.js").Saver} Saver
 * @typedef {import("./installation.js").SaverList} SaverList
 * @typedef {import("./repository-index.js").RepositoryIndex} RepositoryIndex
 *
 * @typedef {object} Activation  One token saver at one owner's level.
 * @property {string} saver  rtk, caveman or codegraph
 * @property {ActivationValue} value  its own value at this level
 * @property {string} summary  what it resolves to and from which level, said
 *           by the server: "inherit — resolves on (project)"
 *
 * @typedef {object} Activations  An owner's token savers.
 * @property {ActivationLevel} level
 * @property {string} ownerId
 * @property {Activation[]} activations
 *
 * @typedef {object} TokenSaverGateway
 * @property {(seed: unknown) => Activations} decodeActivations
 *           A page's Token savers control, as the server seeded it.
 * @property {(level: ActivationLevel, ownerId: string, saver: string, value: ActivationValue) => Promise<Activations>} setActivation
 *           Sets one saver for the owner and answers the owner's
 *           activations as they now read. Rejects with INVALID_INPUT or the
 *           owner's NOT_FOUND code.
 * @property {(seed: unknown) => SaverList} decodeSavers
 *           The Token savers page's savers, as the server seeded them.
 * @property {() => Promise<SaverList>} listSavers
 * @property {(saver: string) => Promise<Saver>} installSaver
 *           Starts installing (or updating to) the pinned version in the
 *           background. Rejects with TOKEN_SAVER_UNSUPPORTED_PLATFORM,
 *           TOKEN_SAVER_BUSY or TOKEN_SAVER_NOT_FOUND.
 * @property {(saver: string) => Promise<Saver>} removeSaver
 *           Rejects with TOKEN_SAVER_BUSY or TOKEN_SAVER_NOT_FOUND.
 * @property {(seed: unknown) => RepositoryIndex[]} decodeIndexes
 *           The repository indexes, as the server seeded them.
 * @property {() => Promise<RepositoryIndex[]>} listIndexes
 * @property {(repositoryId: string) => Promise<RepositoryIndex>} rebuildIndex
 *           Starts a build in the background. Rejects with
 *           TOKEN_SAVER_NOT_INSTALLED, REPOSITORY_NOT_FOUND or INVALID_ROOT.
 * @property {(intensity: CavemanIntensity) => Promise<CavemanIntensity>} setCavemanIntensity
 *           Stores the machine-wide intensity and answers it. Rejects with
 *           INVALID_INPUT.
 *
 * Every decode throws BAD_RESPONSE when its seed is malformed.
 */

export {};
