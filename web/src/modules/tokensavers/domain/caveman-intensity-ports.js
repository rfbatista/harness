// What the Token savers page's Caveman intensity section needs from the
// outside world. Implemented by infrastructure/caveman-intensity-gateway.js
// (over /api).

/**
 * @typedef {"lite" | "full" | "ultra"} CavemanIntensity
 *
 * @typedef {object} CavemanIntensityGateway
 * @property {(intensity: CavemanIntensity) => Promise<CavemanIntensity>} set
 *           Stores the machine-wide intensity and answers it. Rejects with
 *           INVALID_INPUT.
 */

export {};
