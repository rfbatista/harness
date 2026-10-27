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

/** The layers of the hexagonal preset a repository does not have yet, by name. */
export function presetAdditions(existing) {
  const taken = new Set((existing ?? []).map((l) => l.name));
  return hexagonalLayers().filter((l) => !taken.has(l.name));
}

/** Which way a layer matches: "regex" or "folder". */
export const matchKind = (layer) => (typeof layer?.match?.regex === "string" ? "regex" : "folder");

/** The folder or regex a layer matches by. */
export const matchText = (layer) => layer?.match?.[matchKind(layer)] ?? "";

/** How the layer list shows a layer's pattern: a folder as folder/**, a regex between slashes. */
export function layerPattern(layer) {
  const text = matchText(layer);
  if (text === "") return "";
  return matchKind(layer) === "regex" ? `/${text}/` : `${text}/**`;
}

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

/** The access with every layer given `value` for `agent` (or the default), the input left alone. */
export const withRowAccess = (layers, agent, value) =>
  (layers ?? []).map((l) => ({ ...l, access: withAccess(l.access, agent, value) }));

const joinNames = (names) => (names.length < 3 ? names.join(" and ") : `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`);

/** What an access says in words: "everyone reads, only software-architect and devops change". */
export function accessSentence(access) {
  const base = access?.default ?? "read";
  const agents = Object.entries(access?.agents ?? {});
  const named = (level) => agents.filter(([, v]) => v === level).map(([k]) => k).sort(); // sorted: the record keeps its agents in no set order
  if (base === "change") return "everyone reads and changes";
  if (base === "read") {
    const changers = named("change");
    const blind = named("none");
    const parts = [changers.length === 0 ? "everyone reads, nobody changes" : `everyone reads, only ${joinNames(changers)} change`];
    if (blind.length > 0) parts.push(`${joinNames(blind)} cannot read`);
    return parts.join("; ");
  }
  const readers = named("read");
  const changers = named("change");
  const parts = ["nobody reads"];
  if (readers.length > 0) parts[0] += `, only ${joinNames(readers)} ${readers.length === 1 ? "reads" : "read"}`;
  if (changers.length > 0) parts.push(`${joinNames(changers)} can change`);
  return parts.join("; ");
}

/** The colour slots an agent chip picks from; the stylesheet has one colour for each. */
export const AGENT_SLOTS = 8;

/** The slot of an agent's colour, the same for the same name. */
export function agentSlot(name) {
  let h = 0;
  for (const c of String(name ?? "")) h = (h * 31 + c.codePointAt(0)) >>> 0;
  return h % AGENT_SLOTS;
}

/** The overlaps that name a layer. */
export const overlapsOf = (overlaps, name) => (name === "" ? [] : (overlaps ?? []).filter((o) => o.layers.includes(name)));

/** The other layers an overlap names, besides `name`. */
export const otherLayers = (overlap, name) => overlap.layers.filter((l) => l !== name);

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

const slashed = (path) => String(path ?? "").replace(/^\.\//, "").replace(/\/+$/, "");

/** An ignored path as the tree knows it: relative to the checkout, without ./ or a trailing slash. */
function relativeTo(rootDir, path) {
  const root = slashed(rootDir);
  const p = slashed(path);
  return root !== "" && p.startsWith(`${root}/`) ? p.slice(root.length + 1) : p;
}

/**
 * The folders the picker offers: the directories of a `list_tree` answer
 * (relative paths, the root itself left out), sorted, without .git and without
 * the repository's ignored paths or anything below them.
 * @param {{ path: string, is_dir: boolean, children?: any[] } | null} tree
 * @param {string[]} ignored the repository's ignored paths @param {string} rootDir the checkout, for absolute ignored paths
 */
export function folderChoices(tree, ignored, rootDir = "") {
  const skip = (ignored ?? []).map((p) => relativeTo(rootDir, p)).filter((p) => p !== "");
  const hidden = (path) =>
    path.split("/").includes(".git") || skip.some((ig) => path === ig || path.startsWith(`${ig}/`));
  const out = [];
  const walk = (node) => {
    for (const child of node?.children ?? []) {
      if (!child?.is_dir || typeof child.path !== "string" || hidden(child.path)) continue;
      out.push(child.path);
      walk(child);
    }
  };
  walk(tree);
  return out.sort();
}

/** Whether a folder is the one a folder layer matches. */
export const isFolderPicked = (layer, path) => matchKind(layer) === "folder" && matchText(layer) === path;

/**
 * The layer after a folder is checked or unchecked: a folder layer holds one
 * folder, so checking sets it (replacing another) and unchecking the one it
 * holds clears it. The input is left alone.
 */
export function withFolderPicked(layer, path, on) {
  if (on) return { ...layer, match: { folder: path } };
  return isFolderPicked(layer, path) ? { ...layer, match: { folder: "" } } : layer;
}
