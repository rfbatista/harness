// CodeGateway over the harness HTTP API (the code browsing contract's
// /api/code/*) and the project feed's code_changed. The feed's messages are
// bare `data:` lines, so the shared feed's onmessage hears them; other keys
// on the same stream are ignored.

import { diffQuery, sourceQuery, toCodeChanged, toDiff, toFile, toMap, toPaths, toSeed, toTree } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").CodeGateway}
 */
export function codeGateway(api, feed) {
  return {
    async tree(src, dir, signal) {
      return toTree(await api.get("/code/tree", { ...sourceQuery(src), path: dir }, signal));
    },
    async paths(src, signal) {
      return toPaths(await api.get("/code/paths", sourceQuery(src), signal));
    },
    async file(src, path, signal) {
      return toFile(await api.get("/code/file", { ...sourceQuery(src), path }, signal));
    },
    async diff(q, signal) {
      return toDiff(await api.get("/code/diff", diffQuery(q), signal));
    },
    async map(src, signal) {
      return toMap(await api.get("/code/map", sourceQuery(src), signal));
    },

    follow(projectId, onCodeChanged, onStatus) {
      return feed.followPage({ kind: "project", projectId }, {
        onMessage: (msg) => {
          let change;
          try {
            change = toCodeChanged(msg);
          } catch {
            return; // a message we cannot read is dropped; the next change re-reads anyway
          }
          if (change) onCodeChanged(change);
        },
        onStatus,
      });
    },

    decodeSeed: toSeed,
  };
}
