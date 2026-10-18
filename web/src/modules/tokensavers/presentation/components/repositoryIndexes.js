// The Token savers page's Repository indexes section: each repository's
// codegraph index with its age, and Rebuild index. A build runs on the
// server in the background; while one does, the section reads the list
// every half second for its outcome.
//
//   <section x-data="repositoryIndexes" data-seed="repository-indexes-seed"> …

import { describeError } from "../../../../shared/presentation/errors.js";
import { readSeed } from "../../../../shared/presentation/seed.js";
import { indexName, indexState, indexStateText } from "../../domain/repository-index.js";

const POLL_MS = 500;

/** What to do about the server's refusals. */
const NEXT_STEP = {
  TOKEN_SAVER_NOT_INSTALLED: "install codegraph above, then rebuild",
  INVALID_ROOT: "set the repository's root directory in its settings",
};

function described(err) {
  const d = describeError(err);
  return { ...d, next: NEXT_STEP[d.code] ?? d.next };
}

/**
 * @param {{ gateway: import("../../domain/ports.js").TokenSaverGateway,
 *           clock: import("../../../../shared/infrastructure/clock.js").Clock }} deps
 */
export const repositoryIndexes = ({ gateway, clock }) => () => ({
  /** @type {ReturnType<typeof toRow>[]} */
  rows: [],
  loaded: false,
  error: null,
  announcement: "",
  timer: 0,

  async init() {
    try {
      this.apply(gateway.decodeIndexes(readSeed(this.$el)));
    } catch {
      await this.refresh();
    }
    this.follow();
  },

  destroy() {
    clearTimeout(this.timer);
  },

  /** @param {import("../../domain/repository-index.js").RepositoryIndex[]} list */
  apply(list) {
    const now = clock.now();
    this.rows = list.map((idx) => toRow(idx, now));
    this.loaded = true;
  },

  async refresh() {
    try {
      this.apply(await gateway.listIndexes());
    } catch (err) {
      this.error = described(err);
    }
  },

  /** Reads the list again while a build runs. */
  follow() {
    clearTimeout(this.timer);
    if (!this.rows.some((r) => r.building)) return;
    this.timer = setTimeout(async () => {
      const before = new Map(this.rows.map((r) => [r.repositoryId, r.building]));
      await this.refresh();
      for (const r of this.rows) {
        if (!before.get(r.repositoryId) || r.building) continue;
        this.announcement = r.error ? `${r.name}: the index build failed.` : `${r.name}: index built.`;
      }
      this.follow();
    }, POLL_MS);
  },

  async rebuild(repositoryId) {
    this.error = null;
    try {
      const idx = await gateway.rebuildIndex(repositoryId);
      const row = toRow(idx, clock.now());
      this.rows = this.rows.map((r) => (r.repositoryId === idx.repositoryId ? row : r));
      this.follow();
    } catch (err) {
      this.error = described(err);
    }
  },

  dismissError() {
    this.error = null;
  },
});

/** @param {import("../../domain/repository-index.js").RepositoryIndex} idx @param {Date} now */
function toRow(idx, now) {
  return { ...idx, name: indexName(idx), state: indexState(idx), stateText: indexStateText(idx, now) };
}
