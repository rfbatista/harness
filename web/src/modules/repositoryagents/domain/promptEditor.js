// What the repository prompt editor says (UX study, section 6). Pure. The
// preview text itself always comes from the server; this module only decides
// how much of the agent's own prompt to show and what the counters say.

/** The most the server stores; the prompt read says the same (contract section 2). */
export const PROMPT_LIMIT = 8000;

/** The chips: each inserts one example line at the cursor. */
export const CHIPS = Object.freeze([
  { key: "test", label: "+ test command", line: "Run make check before every commit; never skip the race tests." },
  { key: "avoid", label: "+ folders to avoid", line: "Do not touch the migrations folder without asking the architect." },
  { key: "commit", label: "+ commit style", line: "Write commit messages in Portuguese." },
  { key: "ask", label: "+ who to ask", line: "Ask the software architect before changing a contract." },
]);

export const PLACEHOLDER = [
  "Examples:",
  "• Run make check before every commit; never skip the race tests.",
  "• This repo is a Go monolith: new code goes under internal/app, not cmd/.",
  "• Do not touch the migrations folder without asking the architect.",
  "• Write commit messages in Portuguese.",
].join("\n");

const n = (v) => v.toLocaleString("en-US");

/**
 * The character counter under the text area, in words and not only colour.
 * @param {number} length @param {number} [limit]
 */
export function counter(length, limit = PROMPT_LIMIT) {
  if (length > limit) return { text: `${n(length - limit)} over the limit of ${n(limit)} characters`, over: true };
  return { text: `${n(length)} of ${n(limit)} characters · plain text`, over: false };
}

/**
 * Inserts a chip's line at the cursor: on its own line, after what is before
 * the cursor.
 * @param {string} text @param {number} start @param {number} end @param {string} line
 * @returns {{ text: string, cursor: number }}
 */
export function insertLine(text, start, end, line) {
  const before = text.slice(0, start);
  const after = text.slice(end);
  const lead = before === "" || before.endsWith("\n") ? "" : "\n";
  const tail = after === "" || after.startsWith("\n") ? "" : "\n";
  const inserted = `${lead}${line}${tail}`;
  return { text: before + inserted + after, cursor: before.length + lead.length + line.length };
}

const OWN_LINES = 4;

/**
 * The agent's own prompt, collapsed to a few lines unless the person asks for all.
 * @param {string} prompt @param {boolean} expanded
 */
export function ownPrompt(prompt, expanded) {
  const lines = prompt.split("\n");
  const collapsible = lines.length > OWN_LINES;
  return {
    length: prompt.length,
    text: expanded || !collapsible ? prompt : lines.slice(0, OWN_LINES).join("\n") + " …",
    collapsible,
  };
}

/**
 * The line over a preview, and the one that says when nothing is added.
 * @param {string} agent @param {string} repository @param {string} section
 */
export function previewNotes(agent, repository, section) {
  return {
    intro: `The system prompt appended for ${agent} when it starts or resumes in ${repository}. The shaded part is yours; the rest is the agent's own prompt. A session that is already running keeps what it started with.`,
    nothing: section === "" ? "Nothing is added. The agent gets only its own prompt." : "",
  };
}

/**
 * The attention banner over the editor when the agent is not in use here; null when it is.
 * @param {import("./ports.js").AgentRow | undefined} row @param {string} repository @param {string} project
 */
export function notUsedNotice(row, repository, project) {
  if (!row) return null;
  if (row.state === "unassigned") {
    return { text: `${row.name} is unassigned from ${repository}. This prompt is kept but not used. It takes effect again when you assign it again.`, canAssign: true };
  }
  if (row.state === "not_enabled") {
    return { text: `${row.name} is not enabled in ${project}. This prompt is kept but not used.`, canAssign: false };
  }
  return null;
}

/** The footer note of the Save bar. @param {{ dirty: boolean, conflict: boolean, over: boolean }} s */
export function barNote({ dirty, conflict, over }) {
  if (conflict) return "Not saved: this prompt changed since you opened it. Choose what to do with your text.";
  if (over) return "Too long to save.";
  return dirty ? "Unsaved changes" : "No changes";
}
