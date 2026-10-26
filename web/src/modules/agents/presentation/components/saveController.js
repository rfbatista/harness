// Drives the save pipeline (domain/savePipeline.js) for one open editor: the
// debounced dry run after each edit, the save, and Overwrite. The editor says
// what to write through `run`, which sends the form's current values with the
// options it is given (baseVersions, force, dryRun), so the page never builds
// the bytes of a write: the preview is the server's.
//
//   const save = saveController({ run: (o) => gateway.saveBundle(input(), o), onState })
//   save.open(catalog.versions)           // the versions the editor read
//   save.edited({ dirty, valid })         // after every change to the form
//   await save.save()                     // or save({ force: true }) to overwrite

import { describeRefusal } from "../../domain/refusals.js";
import { openSave, reduceSave } from "../../domain/savePipeline.js";

/** How long typing pauses before the preview is asked for. */
export const PREVIEW_DELAY_MS = 350;

/**
 * @param {{
 *   run: (options: import("../../domain/ports.js").WriteOptions) => Promise<import("../../domain/ports.js").WriteResult>,
 *   onState?: (state: import("../../domain/savePipeline.js").SaveState) => void,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 *   delay?: number,
 * }} deps
 */
export function saveController({ run, onState = () => {}, timers = globalThis, delay = PREVIEW_DELAY_MS }) {
  let state = openSave({});
  let timer = null;

  const apply = (event) => {
    state = reduceSave(state, event);
    onState(state);
  };
  const stop = () => {
    if (timer !== null) timers.clearTimeout(timer);
    timer = null;
  };

  async function preview() {
    timer = null;
    const seq = state.seq;
    apply({ type: "preview-start", seq });
    if (state.phase !== "previewing") return;
    try {
      apply({ type: "preview-done", seq, result: await run({ baseVersions: state.baseVersions, dryRun: true }) });
    } catch (err) {
      apply({ type: "preview-failed", seq, refusal: describeRefusal(err) });
    }
  }

  return {
    get state() {
      return state;
    },
    /** A fresh editor, with the versions it read. */
    open(baseVersions) {
      stop();
      state = openSave(baseVersions);
      onState(state);
    },
    /** The form changed: `dirty` against what was read, `valid` to send. */
    edited({ dirty, valid }) {
      stop();
      apply({ type: "edit", dirty });
      if (dirty && valid && state.phase !== "conflict") timer = timers.setTimeout(preview, delay);
    },
    /** @param {{ force?: boolean }} [o] @returns {Promise<{ ok: boolean, result?: import("../../domain/ports.js").WriteResult }>} */
    async save({ force = false } = {}) {
      stop();
      apply({ type: "save-start" });
      try {
        const result = await run({ baseVersions: state.baseVersions, force });
        apply({ type: "saved", result });
        return { ok: true, result };
      } catch (err) {
        apply({ type: "save-refused", refusal: describeRefusal(err) });
        return { ok: false };
      }
    },
    /** Drops the edits and starts from the versions just read. */
    reload(baseVersions) {
      stop();
      apply({ type: "reload", baseVersions });
    },
    /** The editor closed: no preview may fire after it. */
    dispose: stop,
  };
}
