// What the skill form needs (UX spec section 5): the form's starting point
// (empty, a copy of another skill, or the skill being edited), the checks the
// page can make at once (a slug, a free name, a description of at most 1024
// characters), the description coach, Start from, the paths of extra files,
// who a skill reaches, where a refusal lands, and the way back to the agent
// editor the person came from.
//
// The page never builds file bytes: SKILL.md's frontmatter and every preview
// come from the server's dry run (previewModel only lays out what it answered).

import { diffModel } from "./diff.js";
import { slugProblem } from "./names.js";

/** @typedef {import("./ports.js").Catalog} Catalog */
/**
 * @typedef {object} DraftFile
 * @property {string} path  inside the skill's folder, "/" separated
 * @property {string} content  text, or base64 when encoding says so
 * @property {"utf8"|"base64"} encoding
 * @property {number} size  bytes, for the list
 * @property {"existing"|"added"} origin  existing: read from the skill; added: made or uploaded here
 * @property {string | null} original  what an existing file held, to tell an edit
 *
 * @typedef {object} SkillDraft
 * @property {string} previousName  "" for a new skill
 * @property {string} name
 * @property {string} description
 * @property {string} body
 * @property {DraftFile[]} files
 * @property {string[]} deleted  existing files the person removed
 * @property {string[]} bundles  bundles that hold the skill
 * @property {string[]} agents  agents that name it directly
 * @property {{ name: string, description: string, body: string, bundles: string[], agents: string[] }} base  what the form started with
 */

export const DESCRIPTION_LIMIT = 1024;

const enc = encodeURIComponent;
const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const chars = (s) => [...s.trim()].length;

// Start from.

export const START_FROM = Object.freeze([
  { id: "blank", label: "Blank" },
  { id: "workflow", label: "Workflow (steps + checks)" },
  { id: "reference", label: "Reference (rules + examples)" },
  { id: "copy", label: "Copy of another skill…" },
]);

const WORKFLOW = `## When to use
- Say when an agent should reach for this skill.

## Steps
1. First step.
2. Second step.
3. Third step.

## Checks
- How the agent knows each step worked.
`;

const REFERENCE = `## When to use
- Say when an agent should look this up.

## Rules
- First rule, with the reason for it.
- Second rule.

## Examples
\`\`\`
An example the agent can copy.
\`\`\`
`;

/** The body a template starts with; "" for Blank and for a copy, which brings another skill's own. @param {string} id */
export function startFromBody(id) {
  return { workflow: WORKFLOW, reference: REFERENCE }[id] ?? "";
}

// The description coach and its meter.

/** The three points of a good description, each met or not. @param {string} text @param {{ names?: string[], self?: string }} [o] */
export function coachDescription(text, { names = [], self = "" } = {}) {
  const first = text.split(/[.!?—]/)[0].trim();
  const what = first.length >= 12 && first.split(/\s+/).length >= 3;
  const when = /\buse\s+(?:this\s+)?(?:skill\s+)?when\b/i.test(text);
  return [
    { id: "what", ok: what, label: "Says what it does in the first sentence" },
    { id: "when", ok: when, label: "Says when to use it (“Use when…”)" },
    { id: "neighbour", ok: pointsToNeighbour(text, new Set(names.filter((n) => n !== self))), label: "Points to a neighbour skill, so the two are not confused" },
  ];
}

/** Whether the text names another skill the way a pointer does: in backticks, hyphenated, or after "use", "see" … */
function pointsToNeighbour(text, known) {
  if (known.size === 0) return false;
  const lower = text.toLowerCase();
  const patterns = [
    /`([a-z0-9][a-z0-9-]*)`/g,
    /(?<![a-z0-9-])([a-z0-9]+(?:-[a-z0-9]+)+)(?![a-z0-9-])/g,
    /\b(?:use|see|try|prefer|instead of|unlike)\s+(?:the\s+)?[`'"]?([a-z0-9][a-z0-9-]*)/g,
  ];
  return patterns.some((re) => [...lower.matchAll(re)].some((m) => known.has(m[1])));
}

/** @param {string} description */
export function descriptionMeter(description) {
  const count = chars(description);
  return { count, limit: DESCRIPTION_LIMIT, over: count > DESCRIPTION_LIMIT, percent: Math.min(100, (count / DESCRIPTION_LIMIT) * 100), text: `${count} / ${DESCRIPTION_LIMIT}` };
}

// The form's starting point and what has changed.

const fileOf = (f, origin) => ({ path: f.path, content: f.content, encoding: f.encoding === "base64" ? "base64" : "utf8", size: f.size, origin, original: origin === "existing" ? f.content : null });

/**
 * @param {Pick<Catalog, "skills" | "bundles" | "agents">} _catalog
 * @param {{ name?: string, previous?: import("./ports.js").SkillDetail["skill"], from?: import("./ports.js").SkillDetail["skill"] }} source
 *        previous: the skill being edited; from: a skill to copy; name: a name the link suggests
 * @returns {SkillDraft}
 */
export function skillDraft(_catalog, { name = "", previous, from } = {}) {
  const files = (s, origin) => s.files.filter((f) => !f.dir).map((f) => fileOf(f, origin));
  if (previous) {
    const base = { name: previous.name, description: previous.description, body: previous.body, bundles: [...previous.bundles], agents: [...previous.agents] };
    return { previousName: previous.name, name: previous.name, description: previous.description, body: previous.body, files: files(previous, "existing"), deleted: [], bundles: [...previous.bundles], agents: [...previous.agents], base };
  }
  const base = { name, description: "", body: "", bundles: [], agents: [] };
  if (from) return { previousName: "", name, description: from.description, body: from.body, files: files(from, "added"), deleted: [], bundles: [], agents: [], base };
  return { previousName: "", name, description: "", body: "", files: [], deleted: [], bundles: [], agents: [], base };
}

const symmetric = (a, b) => a.filter((x) => !b.includes(x)).length + b.filter((x) => !a.includes(x)).length;

/** How many things differ from what the form started with: each field once, each file, each reader. @param {SkillDraft} d */
export function skillChanges(d) {
  let n = 0;
  if (d.name !== d.base.name) n++;
  if (d.description !== d.base.description) n++;
  if (d.body !== d.base.body) n++;
  for (const f of d.files) if (f.origin === "added" || f.content !== f.original) n++;
  n += d.deleted.length;
  n += symmetric(d.bundles, d.base.bundles) + symmetric(d.agents, d.base.agents);
  return n;
}

/** A free name near `name`: name-2, name-3 … @param {string} name @param {Set<string>} taken */
function suggest(name, taken) {
  for (let i = 2; ; i++) {
    const tail = `-${i}`;
    const candidate = `${name.slice(0, 64 - tail.length).replace(/-+$/, "")}${tail}`;
    if (!taken.has(candidate)) return candidate;
  }
}

/**
 * Whether the form is fit to send and what is wrong with it at once. The
 * description's missing message shows only once the person has touched the form.
 * @param {SkillDraft} draft
 * @param {Pick<Catalog, "skills">} catalog
 * @param {{ touched?: boolean }} [o]
 */
export function checkSkillDraft(draft, catalog, { touched = false } = {}) {
  let nameProblem = draft.name === "" ? "" : slugProblem(draft.name, "Skill name");
  /** @type {{ name: string, href: string, suggestion: string } | null} */
  let taken = null;
  if (draft.name !== "" && nameProblem === "" && draft.name !== draft.previousName) {
    const names = new Set(catalog.skills.map((s) => s.name));
    if (names.has(draft.name)) {
      taken = { name: draft.name, href: `/agents/skills/${enc(draft.name)}`, suggestion: suggest(draft.name, names) };
      nameProblem = `A skill named ${draft.name} already exists.`;
    }
  }
  const length = chars(draft.description);
  let descriptionProblem = "";
  if (length === 0) descriptionProblem = touched ? "Add a description to save." : "";
  else if (length > DESCRIPTION_LIMIT) descriptionProblem = `The description is ${length} characters, ${length - DESCRIPTION_LIMIT} over the limit of ${DESCRIPTION_LIMIT}. Agents read all of it, so cut it down.`;
  return { valid: draft.name !== "" && nameProblem === "" && length > 0 && length <= DESCRIPTION_LIMIT, nameProblem, taken, descriptionProblem };
}

/**
 * The banner that says a rename moves the folder and rewrites its references.
 * @param {Pick<Catalog, "agents" | "bundles">} catalog
 * @param {string} previous  "" for a new skill
 * @param {string} next
 */
export function renameNote(catalog, previous, next) {
  if (!previous || !next || previous === next) return { show: false, places: 0, text: "" };
  const places = [...catalog.agents, ...catalog.bundles].reduce((n, o) => n + o.entries.filter((e) => e === previous).length, 0);
  if (places === 0) return { show: true, places, text: "Renaming moves the folder. Nothing in agents.json names it, so no references change." };
  return { show: true, places, text: `Renaming moves the folder and rewrites the ${plural(places, "place")} in agents.json that ${places === 1 ? "names" : "name"} it. Running sessions keep the old copy until they end.` };
}

/**
 * The agents a skill reaches: the ones picked, and every agent that names a
 * picked bundle, directly or through bundles that hold it.
 * @param {Pick<Catalog, "bundles">} catalog
 * @param {Pick<SkillDraft, "bundles" | "agents">} draft
 */
export function reachOf(catalog, draft) {
  const byName = new Map(catalog.bundles.map((b) => [b.name, b]));
  const agents = new Set(draft.agents);
  const seen = new Set();
  const walk = (name) => {
    const b = byName.get(name);
    if (!b || seen.has(name)) return;
    seen.add(name);
    for (const a of b.usedByAgents) agents.add(a);
    for (const parent of b.usedByBundles) walk(parent);
  };
  draft.bundles.forEach(walk);
  const list = [...agents].sort((a, b) => a.localeCompare(b));
  if (list.length === 0) return { agents: list, none: true, text: "Reaches no agent: the skill stays on the shelf." };
  const shown = list.slice(0, 6).join(", ");
  const more = list.length > 6 ? ` and ${list.length - 6} more` : "";
  return { agents: list, none: false, text: `Reaches ${plural(list.length, "agent")}: ${shown}${more}. Leave both empty to keep the skill on the shelf.` };
}

/**
 * What the save verb is given. Only new and edited files go: the server keeps
 * the rest of the folder, and a rename moves it whole.
 * @param {SkillDraft} d
 */
export function skillSave(d) {
  const files = d.files.filter((f) => f.origin === "added" || f.content !== f.original).map((f) => ({ path: f.path, content: f.content, encoding: f.encoding }));
  return {
    previousName: d.previousName || undefined,
    skill: { name: d.name, description: d.description.trim(), body: d.body, files, deletedFiles: [...d.deleted] },
    addTo: { agents: [...d.agents], bundles: [...d.bundles] },
  };
}

// Extra files.

/** @param {string} path @returns {string[] | null} its segments, or null when it leaves the skill's folder */
function segmentsOf(path) {
  const segs = String(path).trim().replace(/\\/g, "/").split("/").filter((s) => s !== "" && s !== ".");
  return segs.some((s) => s === "..") ? null : segs;
}

/**
 * Where an uploaded file lands inside the skill. A folder upload's own name
 * (the first segment of its relative path) is dropped. SKILL.md, version
 * control and system files are not copied: null.
 * @param {string} path  File.webkitRelativePath for a folder, File.name otherwise
 * @param {{ folder: boolean }} o
 * @returns {string | null}
 */
export function normalizeUploadPath(path, { folder }) {
  const segs = segmentsOf(path);
  if (segs === null) return null;
  if (folder) segs.shift();
  if (segs.length === 0 || segs.includes(".git") || segs[segs.length - 1] === ".DS_Store") return null;
  const joined = segs.join("/");
  return joined === "SKILL.md" ? null : joined;
}

/** A file name the person typed: cleaned, or why it cannot be. @param {string} raw */
export function normalizeNewPath(raw) {
  const segs = segmentsOf(raw);
  if (segs === null) return { path: "", problem: "A file stays inside the skill's folder: no “..”." };
  if (segs.length === 0) return { path: "", problem: "Name the file, for example references/checklist.md." };
  const path = segs.join("/");
  if (path === "SKILL.md") return { path: "", problem: "SKILL.md is written from the form. Pick another name." };
  return { path, problem: "" };
}

/** An uploaded file's bytes as the save takes them: text when it is UTF-8, else base64. @param {Uint8Array} bytes */
export function decodeUpload(bytes) {
  if (!bytes.includes(0)) {
    try {
      return { content: new TextDecoder("utf-8", { fatal: true }).decode(bytes), encoding: /** @type {const} */ ("utf8") };
    } catch {
      // Not text.
    }
  }
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return { content: btoa(binary), encoding: /** @type {const} */ ("base64") };
}

/** @param {number} n */
export function sizeText(n) {
  return n < 1024 ? `${n} B` : n < 1024 * 1024 ? `${Math.round(n / 1024)} KB` : `${(n / 1024 / 1024).toFixed(1)} MB`;
}

// What will be written.

const base64Size = (s) => {
  try {
    return atob(s).length;
  } catch {
    return s.length;
  }
};

/**
 * One panel per file of the server's dry run: a new file, and a SKILL.md,
 * verbatim; anything else as the lines that change.
 * @param {{ path: string, before: string | null, after: string | null, encoding?: string }[]} writes
 */
export function previewModel(writes) {
  const diffs = diffModel(writes);
  return writes.map((w, i) => {
    const badge = w.before === null ? "new" : w.after === null ? "deleted" : "edit";
    const out = { key: w.path, path: w.path, badge, isText: false, isDiff: false, text: "", lines: /** @type {import("./diff.js").DiffLine[]} */ ([]) };
    if (w.encoding === "base64") return { ...out, isText: true, text: `binary file · ${plural(base64Size(w.after ?? w.before ?? ""), "byte")}` };
    if (w.after !== null && (w.before === null || /(^|\/)SKILL\.md$/.test(w.path))) return { ...out, isText: true, text: w.after };
    return { ...out, isDiff: true, lines: diffs[i].lines };
  });
}

// Refusals.

const FIELD_OF = { name: "name", description: "description", files: "files", deleted_files: "files", path: "files" };

/**
 * Which form field a refusal belongs to.
 * @param {import("./refusals.js").Refusal | null} refusal
 */
export function skillFieldErrors(refusal) {
  const out = { name: "", description: "", files: "", form: "", suggestion: "" };
  if (!refusal) return out;
  if (refusal.code === "NAME_TAKEN") {
    out.name = refusal.message;
    out.suggestion = refusal.suggestion;
    return out;
  }
  const field = refusal.code === "INVALID_INPUT" ? FIELD_OF[refusal.field.split(".").pop() ?? ""] : undefined;
  out[field ?? "form"] = refusal.message;
  return out;
}

// The trip back to the agent editor.

/** The way back the agent editor gave, if it is a path of this site; "" otherwise. @param {string | null | undefined} raw */
export function safeReturn(raw) {
  if (typeof raw !== "string" || !raw.startsWith("/") || raw.startsWith("//") || raw.startsWith("/\\")) return "";
  return raw;
}

/**
 * Where to go back to: `returnTo`, with ?added=<skill> before the #fragment.
 * @param {string} returnTo  a safeReturn
 * @param {string} added  "" for Cancel: nothing is added
 */
export function returnHref(returnTo, added) {
  if (added === "") return returnTo;
  const hashAt = returnTo.indexOf("#");
  const hash = hashAt < 0 ? "" : returnTo.slice(hashAt);
  const path = hashAt < 0 ? returnTo : returnTo.slice(0, hashAt);
  const queryAt = path.indexOf("?");
  const pathname = queryAt < 0 ? path : path.slice(0, queryAt);
  const params = new URLSearchParams(queryAt < 0 ? "" : path.slice(queryAt + 1));
  params.delete("added");
  const rest = params.toString();
  return `${pathname}?${rest ? `${rest}&` : ""}added=${enc(added)}${hash}`;
}
