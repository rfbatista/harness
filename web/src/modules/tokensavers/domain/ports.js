// What the tokensavers module needs from the outside world. Implemented by
// infrastructure/tokensavers-gateway.js (over /api).

/**
 * @typedef {"project" | "repository" | "task"} ActivationLevel
 * @typedef {"inherit" | "on" | "off"} ActivationValue
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
 * @property {(seed: unknown) => Activations} decodeSeed
 *           A page's Token savers control, as the server seeded it.
 * @property {(level: ActivationLevel, ownerId: string, saver: string, value: ActivationValue) => Promise<Activations>} set
 *           Sets one saver for the owner and answers the owner's
 *           activations as they now read. Rejects with INVALID_INPUT or the
 *           owner's NOT_FOUND code.
 */

export {};
