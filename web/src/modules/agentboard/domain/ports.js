// What the Agent board needs from the outside world. Implemented by
// infrastructure/agentboard-gateway.js (over /api and the page event stream)
// and infrastructure/memory-gateway.js.

/**
 * @typedef {object} BoardEvent
 * @property {number} seq
 * @property {"session_live"|"session_ended"|"task_changed"|"project_changed"|string} kind
 * @property {string} projectId
 * @property {string} projectName
 * @property {string} taskId
 * @property {string} sessionId
 * @property {string} ended
 * @property {import("./board.js").Card|null} card
 *
 * @typedef {object} AgentBoardGateway
 * @property {(seed: unknown) => object} decodeSeed
 *           Reads the seed the server rendered into the page. Throws BAD_RESPONSE when malformed.
 * @property {() => Promise<object>} readSeed
 *           GET /api/agent_board: the board now, with the feed's seq it reflects.
 * @property {(onEvent: (event: BoardEvent) => void, onStatus?: (status: import("../../../shared/domain/feed.js").FeedStatus) => void) => () => void} follow
 *           The live session feed, server-wide. Returns the function that stops it.
 */

export {};
