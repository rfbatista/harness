// What the settings module needs from the outside world: one boolean global
// setting stored. Implemented by infrastructure/settings-gateway.js over the
// settings API.

/**
 * @typedef {object} SettingsGateway
 * @property {(key: string, on: boolean) => Promise<boolean>} setFlag
 *           Stores the flag under the settings key and answers it as stored.
 *           Rejects with INVALID_INPUT for a key or value the server does
 *           not take.
 * @property {(key: string, on: boolean) => Promise<boolean>} setBranchBlock
 *           Stores a branch block as "on" or "off" and answers whether it is
 *           on as stored. Rejects with INVALID_INPUT when the server refuses.
 * @property {(bytes: number) => Promise<number>} setMemoryBudget
 *           Stores the global orçamento de memória in bytes and answers it
 *           as stored. Rejects with INVALID_INPUT for a value that is not
 *           positive.
 * @property {(level: string, ownerId: string, bytes: number) => Promise<{ bytes: number, effectiveBytes: number }>} setMemoryBudgetOverride
 *           Sets a repository's or task's own orçamento (0 inherits) and
 *           answers it with what a session there now gets. Rejects with
 *           INVALID_INPUT, REPOSITORY_NOT_FOUND or TICKET_NOT_FOUND.
 */

export {};
