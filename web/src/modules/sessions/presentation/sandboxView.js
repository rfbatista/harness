// What the Sandbox tab shows of a session's sandbox facts (UX 7.2), and of
// its violations (UX 7.3), as words.

import { accessOf, hintFor, allowable, allowPathHref, sandboxSettingsHref } from "../domain/sandbox.js";

const DRIVERS = { seatbelt: "Seatbelt (sandbox-exec)", bubblewrap: "Bubblewrap (bwrap)" };

/**
 * Part 1: this session's sandbox.
 * @param {import("../domain/session.js").Session} session
 */
export function factsView(session) {
  const f = session.sandbox;
  const base = { settingsHref: sandboxSettingsHref() };
  if (!f) return { ...base, kind: "legacy", status: "", unsandboxed: false, driver: "", repositories: [] };
  if (!f.sandboxed) return { ...base, kind: "off", status: "Ran without the sandbox.", unsandboxed: true, driver: "", repositories: [] };
  return {
    ...base,
    kind: "on",
    status: "Ran sandboxed",
    unsandboxed: false,
    driver: DRIVERS[f.driver] ?? f.driver,
    repositories: repositoriesOf(session.mode, f.visibility),
  };
}

function repositoriesOf(mode, visibility) {
  if (visibility === "own repo") return [{ name: "this worktree", access: "read and write" }];
  if (mode === "architect") return [{ name: "this worktree", access: "read and write" }, { name: "every other repository of the project", access: "read-only" }];
  return [{ name: "every repository of the project", access: "read-only" }];
}

const TIME = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });

/** A row of the list, as the markup binds it. */
export function rowView(row) {
  const span = row.count > 1 ? `${row.count}× between ${TIME.format(row.first)} and ${TIME.format(row.last)}` : `at ${TIME.format(row.last)}`;
  const offer = allowable(row);
  return {
    key: row.key,
    time: TIME.format(row.last),
    stamp: row.last.toLocaleString(),
    access: row.access,
    path: row.path === "" ? "(none)" : row.path,
    process: row.process,
    count: row.count > 1 ? `×${row.count}` : "",
    denied: `Denied: ${row.operation}, by ${row.process}, ${span}`,
    hint: hintFor(row),
    allowHref: offer ? allowPathHref(row.path) : "",
    plain: `${row.last.toISOString()} ${accessOf(row.operation)} ${row.path} ${row.process}${row.count > 1 ? ` x${row.count}` : ""}`,
  };
}
