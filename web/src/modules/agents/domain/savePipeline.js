// The save pipeline every catalog editor shares (UX spec section 6): a pure
// state machine for the phases of one open editor, and the view the footer,
// the Review change panel, the conflict banner and the toast show.
//
//   clean      nothing changed since the editor read the catalog
//   dirty      there is an edit; the preview may not be up to date
//   previewing a dry run is in flight
//   conflict   a write or preview was refused with CATALOG_CHANGED: Save is off
//   saved      the last save went through
//   error      the server refused it for another reason (a field, a cycle …)
//
// The versions the editor read when it opened (`baseVersions`) are kept until
// it saves, which replaces them with the versions the write answered, or
// reloads. The page never computes file bytes: the preview is the dry run.

import { conflictModel, diffModel } from "./diff.js";

/**
 * @typedef {import("./refusals.js").Refusal} Refusal
 * @typedef {import("./ports.js").WriteResult} WriteResult
 *
 * @typedef {object} SaveState
 * @property {"clean"|"dirty"|"previewing"|"conflict"|"saved"|"error"} phase
 * @property {Record<string, string>} baseVersions
 * @property {number} seq  counts edits; a preview answer for an older one is dropped
 * @property {{ writes: WriteResult["change"]["writes"], summary: string, projects: WriteResult["change"]["projects"] } | null} preview
 * @property {Record<string, string | null>} seen  the text each file had when last previewed
 * @property {Refusal | null} refusal
 * @property {{ changed: Refusal["changed"], currentVersions: Record<string, string> } | null} conflict
 * @property {{ id: string, summary: string } | null} saved
 * @property {boolean} saving
 */

/** @param {Record<string, string>} baseVersions @returns {SaveState} */
export function openSave(baseVersions) {
  return { phase: "clean", baseVersions: { ...baseVersions }, seq: 0, preview: null, seen: {}, refusal: null, conflict: null, saved: null, saving: false };
}

/** The refusal, as the phase it puts the editor in. @param {SaveState} s @param {Refusal} refusal */
function refused(s, refusal) {
  if (refusal.code === "CATALOG_CHANGED") {
    return { ...s, phase: "conflict", saving: false, refusal: null, conflict: { changed: refusal.changed, currentVersions: refusal.currentVersions } };
  }
  return { ...s, phase: "error", saving: false, refusal };
}

/**
 * @param {SaveState} s
 * @param {{ type: string, [k: string]: any }} e
 * @returns {SaveState}
 */
export function reduceSave(s, e) {
  switch (e.type) {
    case "edit": {
      const phase = s.phase === "conflict" ? "conflict" : e.dirty ? "dirty" : "clean";
      return { ...s, phase, seq: s.seq + 1, preview: null, refusal: null, saved: null, saving: false };
    }
    case "preview-start":
      if (e.seq !== s.seq || s.phase === "conflict") return s;
      return { ...s, phase: "previewing" };
    case "preview-done": {
      if (e.seq !== s.seq) return s;
      const { writes, summary, projects = [] } = e.result.change;
      const seen = { ...s.seen };
      for (const w of writes) seen[w.path] = w.before;
      return { ...s, phase: s.phase === "conflict" ? "conflict" : "dirty", preview: { writes, summary, projects }, seen, refusal: null };
    }
    case "preview-failed":
      if (e.seq !== s.seq) return s;
      return refused(s, e.refusal);
    case "save-start":
      return { ...s, saving: true };
    case "saved":
      return {
        ...s,
        phase: "saved",
        saving: false,
        baseVersions: { ...s.baseVersions, ...e.result.versions },
        preview: null,
        refusal: null,
        conflict: null,
        saved: { id: e.result.change.id, summary: e.result.change.summary },
      };
    case "save-refused":
      return refused(s, e.refusal);
    case "reload":
      return openSave(e.baseVersions);
    case "outside": {
      // The files the editor read changed on disk, found by a live re-read, not by a refused write.
      const changed = Object.keys(e.currentVersions).filter((p) => p in s.baseVersions && e.currentVersions[p] !== s.baseVersions[p]).map((path) => ({ path, theirs: undefined }));
      return { ...s, phase: "conflict", saving: false, refusal: null, preview: null, conflict: { changed, currentVersions: e.currentVersions } };
    }
    case "rebase":
      // Another item changed in a file this editor writes; this one did not. Take the new versions, and ask for the preview again.
      if (s.phase === "conflict" || s.phase === "saved") return s;
      return { ...s, baseVersions: { ...s.baseVersions, ...e.versions }, seq: s.seq + 1, preview: null, phase: s.phase === "previewing" ? "dirty" : s.phase };
    default:
      return s;
  }
}

/**
 * Whether Save is on: something valid to save, no conflict, nothing in flight.
 * @param {SaveState} s
 * @param {boolean} valid  whether the form itself is fit to send
 * @param {boolean} [blocked]  saving is off for a reason outside the form: the catalog does not parse
 */
export function canSave(s, valid, blocked = false) {
  return valid && !blocked && !s.saving && (s.phase === "dirty" || s.phase === "previewing");
}

// The diffs are worked out once per preview or conflict, not on every read of the view.
const diffs = new WeakMap();
const memo = (key, make) => {
  if (!diffs.has(key)) diffs.set(key, make());
  return diffs.get(key);
};

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/**
 * What the sticky footer, the Review change panel and the conflict banner show.
 * @param {SaveState} s
 * @param {{ valid: boolean, changes: number, writes: string[], blocked?: string }} form  blocked: why saving is off (a file that does not parse), or ""
 */
export function saveView(s, form) {
  const paths = s.preview ? s.preview.writes.map((w) => w.path) : form.writes;
  const writes = paths.join(", ");
  const dirty = s.phase !== "clean" && s.phase !== "saved";
  let footer;
  if (form.blocked) footer = `Saving is off · ${form.blocked}`;
  else if (!dirty) footer = `No changes · writes ${writes}`;
  else if (!form.valid) footer = `${plural(Math.max(form.changes, 1), "unsaved change", "unsaved changes")} · fix the form to save`;
  else footer = `${plural(Math.max(form.changes, 1), "unsaved change", "unsaved changes")} · writes ${writes}`;
  const conflict = s.phase === "conflict" && s.conflict !== null;
  return {
    phase: s.phase,
    dirty,
    saving: s.saving,
    previewing: s.phase === "previewing",
    footer,
    canSave: canSave(s, form.valid, Boolean(form.blocked)),
    review: s.preview !== null,
    files: s.preview ? memo(s.preview, () => diffModel(s.preview.writes)) : [],
    conflict,
    conflictTitle: conflict ? `${s.conflict.changed.map((c) => c.path).join(", ") || "A file"} changed on disk while you were editing.` : "",
    conflictFiles: conflict ? memo(s.conflict, () => conflictModel(s.conflict.changed, s.seen)) : [],
    conflictNote: conflict
      ? "Reload shows the file as it is now and drops your unsaved changes. Overwrite saves your version over it, so what changed on disk is lost. There is no merge."
      : "",
    refusal: s.refusal,
    blocked: Boolean(form.blocked),
  };
}

/** The Undo that follows a save, and what it does not reach. @param {{ id: string, summary: string }} saved */
export function savedToast(saved) {
  return {
    text: saved.summary,
    undoId: saved.id,
    tone: "",
    note: "Changes reach sessions from their next start. Running sessions keep what they started with.",
  };
}

/** The toast after Undo went through. @param {string} summary */
export function undoneToast(summary) {
  return { text: summary || "Undid the last change", undoId: "", tone: "", note: "Running sessions keep what they started with." };
}

/** The toast when Undo is refused: the reason, in the refusal's words. @param {Refusal} refusal */
export function refusedUndoToast(refusal) {
  return { text: refusal.message, undoId: "", tone: "danger", note: "" };
}
