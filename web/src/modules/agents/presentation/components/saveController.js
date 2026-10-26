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
 *   blocked?: () => boolean,
 * }} deps  blocked: saving is off (the catalog does not parse), so no preview is asked for
 */
export function saveController({ run, onState = () => {}, timers = globalThis, delay = PREVIEW_DELAY_MS, blocked = () => false }) {
  let state = openSave({});
  let timer = null;
  let ready = false;

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
      ready = valid;
      if (dirty && valid && state.phase !== "conflict" && !blocked()) timer = timers.setTimeout(preview, delay);
    },
    /**
     * The file this editor writes changed on disk, and the open item did not:
     * take the versions now, and ask again for the preview an edit was owed.
     * @param {Record<string, string>} versions
     */
    rebase(versions) {
      stop();
      apply({ type: "rebase", versions });
      if (state.phase === "dirty" && ready && !blocked()) timer = timers.setTimeout(preview, delay);
    },
    /** Saving was off and is on again: ask for the preview an edit was owed. */
    resume() {
      if (timer === null && state.phase === "dirty" && state.preview === null && ready && !blocked()) timer = timers.setTimeout(preview, delay);
    },
    /**
     * The open item changed on disk. A dry run says what the server says (a
     * CATALOG_CHANGED refusal carries the file's text); without that answer
     * the editor still blocks Save and says the files changed.
     * @param {Record<string, string>} currentVersions
     */
    async probe(currentVersions) {
      stop();
      const seq = state.seq;
      try {
        await run({ baseVersions: state.baseVersions, dryRun: true });
      } catch (err) {
        const refusal = describeRefusal(err);
        if (seq !== state.seq) return;
        if (refusal.code === "CATALOG_CHANGED") return apply({ type: "preview-failed", seq, refusal });
      }
      if (seq === state.seq) apply({ type: "outside", currentVersions });
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
