// What the Token savers page needs from the outside world. Implemented by
// infrastructure/installations-gateway.js (over /api).

/**
 * @typedef {import("./installation.js").Saver} Saver
 * @typedef {import("./installation.js").SaverList} SaverList
 *
 * @typedef {object} TokenSaverInstallGateway
 * @property {(seed: unknown) => SaverList} decodeSeed
 *           The page's savers, as the server seeded them.
 * @property {() => Promise<SaverList>} list
 * @property {(saver: string) => Promise<Saver>} install
 *           Starts installing (or updating to) the pinned version in the
 *           background. Rejects with TOKEN_SAVER_UNSUPPORTED_PLATFORM,
 *           TOKEN_SAVER_BUSY or TOKEN_SAVER_NOT_FOUND.
 * @property {(saver: string) => Promise<Saver>} remove
 *           Rejects with TOKEN_SAVER_BUSY or TOKEN_SAVER_NOT_FOUND.
 */

export {};
