// Where a refusal lands. The store names the entry it refused ("guard.protected_paths[1]",
// "review.visual_qa.ui.routes[2]"); the page keeps one error per row, so such an error lands on the row that
// holds the field and says which entry in its message.

import type { FieldError } from "./settings";

/** The entry a suffix of a field path names, in the person's words, or the suffix itself. */
function where(suffix: string): string {
  const entry = /^\[(\d+)\]$/.exec(suffix);
  if (entry) return `entry ${Number(entry[1]) + 1}`;
  const list = /^\.(routes|viewports|ui_paths)\[(\d+)\]$/.exec(suffix);
  if (list) return `${{ routes: "line", viewports: "viewport", ui_paths: "UI path" }[list[1] as "routes" | "viewports" | "ui_paths"]} ${Number(list[2]) + 1}`;
  if (suffix === ".url") return "URL";
  if (suffix === ".start_command") return "start command";
  return suffix;
}

/** The error as it lands: on the longest of `rows` that holds its field, naming the entry; unchanged when it names a row itself or none. */
export function landOnRow(error: FieldError, rows: readonly string[]): FieldError {
  const holders = rows.filter((row) => error.field.startsWith(row) && (error.field[row.length] === "[" || error.field[row.length] === "."));
  const row = holders.sort((a, b) => b.length - a.length)[0];
  if (row === undefined) return error;
  return { ...error, field: row, message: `${error.message} (${where(error.field.slice(row.length))})` };
}

/** Errors by row: several on one row read as one message. */
export function byRow(errors: readonly FieldError[], rows: readonly string[]): Record<string, FieldError> {
  const out: Record<string, FieldError> = {};
  for (const error of errors) {
    const landed = landOnRow(error, rows);
    const earlier = out[landed.field];
    out[landed.field] = earlier === undefined ? landed : { ...earlier, message: `${earlier.message} · ${landed.message}` };
  }
  return out;
}
