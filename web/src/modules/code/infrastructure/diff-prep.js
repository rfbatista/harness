// What the code worker does with a diff page, as a plain module: maps the
// wire answer, builds each file's rows for the layout, highlights them, and
// says whether a file changed since the page it last saw. The worker entry
// (web/src/code-worker.js) wires it to messages; the unit tests and the
// in-process preparer run it directly. Never touches the DOM.
//
// Messages (the seam between the main thread and the worker):
//
//   main → worker
//     { type: "prepare", id, page, layout }   a diff page in the wire shape, and { split } for its rows
//     { type: "layout", id, layout }           the same files again, for another layout
//   worker → main (every message carries the id it answers)
//     { type: "diff", id, diff }               the page's answer without hunks: CodeDiff with PreparedMeta files
//     { type: "file", id, path, file }         one PreparedFile, in page order, as soon as it is ready
//     { type: "done", id }
//     { type: "error", id, code, message }

import { HIGHLIGHT_DIFF_ROWS, SPLIT_UP_TO } from "../domain/lines.js";
import { largestLine } from "../domain/diff.js";
import { diffRows, tokenise } from "../domain/rows.js";
import { toDiff } from "./dto.js";

/**
 * @typedef {object} PreparedFile
 * @property {string} path
 * @property {import("../domain/rows.js").Row[]} rows  with their tokens on each diff row
 * @property {boolean} split       the rows are the split layout's
 * @property {boolean} changed     the file's hunks differ from the page this worker saw last (always true for a new file)
 * @property {boolean} plain       too long to highlight, or no grammar
 */

/** A change's identity: everything a section shows. */
function signature(f) {
  const parts = [f.status, f.oldPath, f.added, f.deleted, f.withheld ?? "", f.binary ? "b" : ""];
  for (const h of f.hunks ?? []) {
    parts.push(`@${h.oldStart},${h.oldLines},${h.newStart},${h.newLines}${h.header}`);
    for (const l of h.lines) parts.push(`${l.kind[0]}${l.old ?? ""}:${l.new ?? ""}${l.noNewline ? "!" : ""}${l.text}`);
  }
  return parts.join("\n");
}

/** A file's diff without its hunks: what the main thread keeps and lists. */
function meta(f) {
  return Object.freeze({ ...f, hunks: null, hasHunks: f.hunks !== null, largest: largestLine(f.hunks) });
}

/** @param {import("../domain/ports.js").Highlighter} highlighter */
export function diffPreparer(highlighter) {
  /** @type {Map<string, { file: import("../domain/code.js").FileDiff, sig: string }>} */
  const kept = new Map();

  function rowsOf(file, layout) {
    const split = layout.split && file.added + file.deleted <= SPLIT_UP_TO;
    const rows = diffRows(file.hunks, split ? "split" : "unified");
    let plain = !highlighter.supports(file.language) || rows.length > HIGHLIGHT_DIFF_ROWS;
    if (!plain) {
      for (const [row, tokens] of tokenise(rows, highlighter, file.language)) row.tokens = tokens;
    }
    return { rows, split, plain };
  }

  function prepared(file, layout, changed) {
    return { path: file.path, ...rowsOf(file, layout), changed };
  }

  return {
    /**
     * @param {unknown} page
     * @returns {{ diff: import("../domain/code.js").CodeDiff, files: Generator<PreparedFile> }}
     */
    prepare(page, layout) {
      const diff = toDiff(page);
      const files = diff.files;
      const changed = files.map((f) => {
        const sig = signature(f);
        const before = kept.get(f.path);
        kept.set(f.path, { file: f, sig });
        return !before || before.sig !== sig;
      });
      return {
        diff: Object.freeze({ ...diff, files: Object.freeze(files.map(meta)) }),
        *files() {
          for (let i = 0; i < files.length; i += 1) yield prepared(files[i], layout, changed[i]);
        },
      };
    },

    /** Every file kept, again for another layout; none of them counts as changed. */
    *relayout(layout) {
      for (const { file } of kept.values()) if (file.hunks) yield prepared(file, layout, false);
    },
  };
}

/**
 * The message handler of a worker: answers each request through `post`.
 * @param {ReturnType<typeof diffPreparer>} preparer
 * @param {(msg: object) => void} post
 * @param {() => Promise<void>|void} [later]  awaited between files; a worker needs none, since posting one file does not wait for the next
 */
export function serve(preparer, post, later = () => {}) {
  const fail = (id, err) => post({ type: "error", id, code: err?.code ?? "BAD_RESPONSE", message: String(err?.message ?? err) });
  async function stream(id, files) {
    try {
      for (const file of files) {
        post({ type: "file", id, path: file.path, file });
        await later();
      }
      post({ type: "done", id });
    } catch (err) {
      fail(id, err);
    }
  }
  return (msg) => {
    const id = msg?.id;
    try {
      if (msg.type === "prepare") {
        const { diff, files } = preparer.prepare(msg.page, msg.layout);
        post({ type: "diff", id, diff });
        stream(id, files());
      } else if (msg.type === "layout") {
        stream(id, preparer.relayout(msg.layout));
      }
    } catch (err) {
      fail(id, err);
    }
  };
}
