// What the usage module needs from the outside world. Implemented by
// infrastructure/usage-gateway.js over the usage API and its event stream.

/**
 * @typedef {import("./usage.js").UsageSummary} UsageSummary
 * @typedef {import("./usage.js").UsageRecord} UsageRecord
 * @typedef {import("./usage.js").Range} Range
 * @typedef {import("./usage.js").Totals} Totals
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {object} UsageGateway
 * @property {(seed: unknown) => { projectId: string, summary: UsageSummary|null, records: UsageRecord[] }} decodeSeed
 *           The Usage page's seed, as the server embeds it.
 * @property {(seed: unknown) => { projectId: string, taskId: string, task: Totals|null, sessions: Record<string, Totals> }} decodeTotalsSeed
 *           A task page's usage totals seed, as the server embeds it.
 * @property {(query: { taskId?: string, sessionId?: string }, signal?: AbortSignal) => Promise<Totals>} totals
 *           One task's or one session's totals over all time.
 * @property {(projectId: string, range: Range, signal?: AbortSignal) => Promise<UsageSummary>} summary
 *           The project's totals, tokens over time by model and top tasks and
 *           sessions over the range.
 * @property {(projectId: string, signal?: AbortSignal) => Promise<UsageRecord[]>} recent
 *           The project's recent usage records, newest first.
 * @property {(projectId: string,
 *             onRecord: (record: UsageRecord) => void,
 *             onStatus: (status: FeedStatus) => void) => () => void} follow
 *           Each usage record stored for the project from now on, until the
 *           returned function is called.
 */

export {};
