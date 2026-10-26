// Layers and who may read and change them, as the record holds them (the
// contract "Repository architecture record and architecture file"). Pure.

import { PLAIN } from "./diagram.js";

export { PLAIN };

/** The levels of access, in the order a cell cycles through them. */
export const ACCESS_LEVELS = Object.freeze(["none", "read", "change"]);

/** The column of the matrix that is no layer: what applies to paths in none. */
export const UNLAYERED = "unlayered";

/** The row of the matrix that is no agent: a layer's access for agents it does not list. */
export const DEFAULT_ROW = "default";

/** The access a new layer starts with: everyone reads, only the architect changes. */
export const newAccess = () => ({ default: "read", agents: { "software-architect": "change" } });

/** A layer for a folder, with the default access and a name no other layer has. */
export function newLayer(existing, kind = "folder") {
  const taken = new Set((existing ?? []).map((l) => l.name));
  let n = taken.size + 1;
  while (taken.has(`layer-${n}`)) n++;
  return { name: `layer-${n}`, match: { [kind]: "" }, access: newAccess() };
}

/** The layers of a hexagonal repository, each with the default access. */
export const hexagonalLayers = () => [
  { name: "domain", match: { folder: "internal/domain" } },
  { name: "ports", match: { folder: "internal/ports" } },
  { name: "application", match: { folder: "internal/app" } },
  { name: "adapters-in", match: { folder: "internal/adapter/in" } },
  { name: "adapters-out", match: { folder: "internal/adapter/out" } },
  { name: "web", match: { folder: "web" } },
  { name: "tests", match: { regex: "(^tests/|_test\\.go$|\\.test\\.js$)" } },
  { name: "docs", match: { folder: "docs" } },
].map((layer) => ({ ...layer, access: newAccess() }));

/** Which way a layer matches: "regex" or "folder". */
export const matchKind = (layer) => (typeof layer?.match?.regex === "string" ? "regex" : "folder");

/** The folder or regex a layer matches by. */
export const matchText = (layer) => layer?.match?.[matchKind(layer)] ?? "";

/** A layer matching by the same text the other way. */
export const withMatchKind = (layer, kind) => ({ ...layer, match: { [kind]: matchText(layer) } });

/** A layer matching by `text`, the way it already matches. */
export const withMatchText = (layer, text) => ({ ...layer, match: { [matchKind(layer)]: text } });

/**
 * What is wrong with a pattern, said at once while it is typed: "" when it is
 * fine or still empty. The server's preview has the final word (RE2).
 */
export function patternProblem(kind, text) {
  const value = String(text ?? "");
  if (value === "") return "";
  if (kind === "regex") {
    try {
      new RegExp(value);
    } catch (err) {
      return `The regex does not compile: ${String(err?.message ?? err).replace(/^Invalid regular expression: /, "")}`;
    }
    return "";
  }
  if (value.startsWith("/")) return "A folder is relative to the repository: no leading /.";
  if (value.split("/").includes("..")) return "A folder cannot contain ..";
  if (value === ".git" || value.startsWith(".git/")) return "A folder cannot be inside .git.";
  return "";
}

/** Whether two layers share a name with an earlier one. */
export const isDuplicateName = (layers, i) =>
  (layers ?? []).slice(0, i).some((l) => l.name !== "" && l.name === layers[i].name);

/** A list with the item at `i` moved by `delta` places, or the list itself when it cannot move. */
export function moved(list, i, delta) {
  const j = i + delta;
  if (j < 0 || j >= list.length || i < 0 || i >= list.length) return list;
  const out = list.slice();
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

/** The access of an agent: its own entry, else the default. */
export const accessOf = (access, agent) => access?.agents?.[agent] ?? access?.default ?? "read";

/**
 * The access with one cell set. An agent whose entry equals the default has
 * no entry; the default itself is the DEFAULT_ROW.
 */
export function withAccess(access, agent, value) {
  const base = { default: access?.default ?? "read", agents: { ...(access?.agents ?? {}) } };
  if (agent === DEFAULT_ROW) {
    base.default = value;
    return base;
  }
  if (value === base.default) delete base.agents[agent];
  else base.agents[agent] = value;
  return base;
}

/** The level after `value` when a cell is pressed: none, read, change, none… */
export const nextAccess = (value) => ACCESS_LEVELS[(ACCESS_LEVELS.indexOf(value) + 1) % ACCESS_LEVELS.length];

/** What a cell is called to a screen reader: "go-developer on domain: change". */
export const cellLabel = (who, where, value) => `${who} on ${where}: ${value}`;

/**
 * The agents the matrix has a row for: the catalog, then any the record names
 * that the catalog lacks (so a stale one stays visible), then plain.
 */
export function matrixAgents(catalog, record) {
  const seen = new Set();
  const out = [];
  const add = (name) => {
    if (typeof name === "string" && name !== "" && name !== PLAIN && !seen.has(name)) {
      seen.add(name);
      out.push(name);
    }
  };
  (catalog ?? []).forEach(add);
  for (const l of record?.layers ?? []) Object.keys(l.access?.agents ?? {}).forEach(add);
  Object.keys(record?.unlayered?.agents ?? {}).forEach(add);
  return [...out, PLAIN];
}

/**
 * The preview answer in the shape the tab reads it: one entry per layer by
 * position, the unlayered files and the overlaps with their winner.
 */
export function readPreview(body) {
  const layers = (Array.isArray(body?.layers) ? body.layers : []).map((l) => ({
    name: l?.name ?? "",
    fileCount: Number.isInteger(l?.file_count) ? l.file_count : 0,
    sample: Array.isArray(l?.sample) ? l.sample : [],
    error: l?.error ? { code: l.error.code ?? "", message: l.error.message ?? "" } : null,
  }));
  const un = body?.unlayered ?? {};
  return {
    layers,
    unlayered: { fileCount: Number.isInteger(un.file_count) ? un.file_count : 0, sample: Array.isArray(un.sample) ? un.sample : [] },
    overlaps: (Array.isArray(body?.overlaps) ? body.overlaps : []).map((o) => ({
      path: o?.path ?? "",
      layers: Array.isArray(o?.layers) ? o.layers : [],
      winner: o?.winner ?? "",
    })),
  };
}

const files = (n) => `${n} ${n === 1 ? "file" : "files"}`;
export const fileCountText = files;

/** "internal/ports/x.go is in ports, domain; ports wins" as the parts a row shows. */
export const overlapText = (o) => `${o.layers.join(", ")} · ${o.winner} wins`;
