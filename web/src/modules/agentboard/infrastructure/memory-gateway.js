// AgentBoardGateway in memory, for tests and web/dev pages: the test plays the
// server with publish() (a feed event, as the wire carries it) and
// reportStatus(), and decides when a seed read answers.

import { FeedStatus } from "../../../shared/domain/feed.js";
import { toEvent, toSeed } from "./dto.js";

/** @param {{ seed?: object }} [options]  seed: the wire body GET /api/agent_board answers */
export function memoryBoard({ seed = null } = {}) {
  let body = seed;
  const followers = new Set();
  /** @type {{ resolve: Function, reject: Function }[]} */
  const waiting = [];
  let hold = false;
  let failure = null;

  const gateway = {
    decodeSeed: toSeed,

    async readSeed() {
      reads.count++;
      if (failure) {
        const err = failure;
        failure = null;
        throw err;
      }
      if (hold) return new Promise((resolve, reject) => waiting.push({ resolve, reject }));
      return toSeed(body);
    },

    follow(onEvent, onStatus = () => {}) {
      const follower = { onEvent, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  const reads = { count: 0 };
  return {
    gateway,
    reads,
    /** Followers now: a page that left must leave none. */
    followers: () => followers.size,
    /** What the next seed read answers. */
    setSeed(next) {
      body = next;
    },
    /** The next seed read rejects with err. */
    failNext(err) {
      failure = err;
    },
    /** Seed reads wait for answer() until release(). */
    holdReads() {
      hold = true;
    },
    /** Answers the waiting seed reads with the current seed. */
    answer() {
      hold = false;
      for (const w of waiting.splice(0)) w.resolve(toSeed(body));
    },
    /** A feed event as the server sends it (wire shape). */
    publish(wire) {
      const event = toEvent(wire);
      for (const f of [...followers]) f.onEvent(event);
    },
    reportStatus(status) {
      for (const f of [...followers]) f.onStatus(status);
    },
  };
}
