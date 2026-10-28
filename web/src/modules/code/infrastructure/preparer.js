// The main thread's side of the code worker: DiffPreparer over a channel.
// The channel is a real Worker (workerPreparer) or the same preparer run in
// this thread (inlinePreparer, for the unit tests and wherever no worker can
// start); both speak diff-prep.js's messages.

import { StructuredError } from "../../../shared/domain/errors.js";
import { diffPreparer, serve } from "./diff-prep.js";

/**
 * @param {{ postMessage: (msg: object) => void, onmessage: ((e: { data: any }) => void) | null, terminate?: () => void }} channel
 * @returns {import("../domain/ports.js").DiffPreparer}
 */
export function preparerOver(channel) {
  let seq = 0;
  const jobs = new Map();
  channel.onmessage = ({ data: msg }) => {
    const job = jobs.get(msg.id);
    if (!job) return;
    if (msg.type === "diff") job.diff = msg.diff;
    else if (msg.type === "file") job.onFile?.(msg.file);
    else if (msg.type === "done") {
      jobs.delete(msg.id);
      job.resolve(job.diff);
    } else if (msg.type === "error") {
      jobs.delete(msg.id);
      job.reject(new StructuredError(msg.code, msg.message));
    }
  };
  const run = (request, onFile) =>
    new Promise((resolve, reject) => {
      const id = ++seq;
      jobs.set(id, { resolve, reject, onFile, diff: null });
      channel.postMessage({ ...request, id });
    });
  return {
    prepare: (page, layout, { onFile } = {}) => run({ type: "prepare", page, layout }, onFile),
    relayout: (layout, { onFile } = {}) => run({ type: "layout", layout }, onFile),
    destroy() {
      for (const job of jobs.values()) job.reject(new DOMException("destroyed", "AbortError"));
      jobs.clear();
      channel.terminate?.();
    },
  };
}

/** The code worker (the second bundle entry, served beside app.js). */
export function workerPreparer(url) {
  return preparerOver(new Worker(url, { name: "code" }));
}

/** The worker's work in this thread, a microtask per file: what the unit tests use. */
export function inlinePreparer(highlighter) {
  const toMain = { onmessage: null };
  const handle = serve(diffPreparer(highlighter), (msg) => toMain.onmessage?.({ data: structuredClone(msg) }), () => Promise.resolve());
  const channel = {
    postMessage: (msg) => queueMicrotask(() => handle(structuredClone(msg))),
    set onmessage(fn) {
      toMain.onmessage = fn;
    },
  };
  return preparerOver(channel);
}

/** A preparer that starts its worker on first use, so pages with no code view do not pay for one. */
export function lazyPreparer(create) {
  let started = null;
  const it = () => (started ??= create());
  return {
    prepare: (page, layout, opts) => it().prepare(page, layout, opts),
    relayout: (layout, opts) => it().relayout(layout, opts),
    destroy: () => started?.destroy(),
  };
}
