// Hook rules of the repository architecture record: grouping by lifecycle
// point, the mode in words and glyphs, new rules, and the text an agent sees
// when a rule fires (the agent-sessions contract, word for word). Pure.

export const MODES = Object.freeze({
  blocking: {
    glyph: "✕",
    label: "Blocking",
    text: "✕ Blocking",
    tone: "danger",
    hint: "The action does not run. The agent gets the message and must change course.",
  },
  warning: {
    glyph: "!",
    label: "Warning only",
    text: "! Warning only",
    tone: "attention",
    hint: "The action runs. The agent sees the message and the event is logged for review.",
  },
});

/** What the When select offers: the two kinds of rule the record holds. */
export const RULE_KINDS = Object.freeze([
  { value: "edit_outside_layer", label: "The agent edits a path outside the layers it may change" },
  { value: "command", label: "A command exits with an error" },
]);

export const LIFECYCLE_POINTS = Object.freeze([
  { value: "before_tool", label: "Before a tool call" },
  { value: "after_edit", label: "After a file edit" },
  { value: "before_commit", label: "Before a commit" },
]);

const GROUPS = [
  { key: "before_tool_and_commit", title: "Before a tool call and at commit", pick: (r) => r.kind === "edit_outside_layer" },
  { key: "before_tool", title: "Before a tool call", pick: (r) => r.kind === "command" && r.at === "before_tool" },
  { key: "after_edit", title: "After a file edit", pick: (r) => r.kind === "command" && r.at === "after_edit" },
  { key: "before_commit", title: "Before a commit", pick: (r) => r.kind === "command" && r.at === "before_commit" },
  { key: "unplaced", title: "No lifecycle point yet", pick: (r) => r.kind === "command" && !["before_tool", "after_edit", "before_commit"].includes(r.at) },
];

/** The non-empty groups, each with its rules and their index in the record. */
export function groupRules(rules) {
  const list = Array.isArray(rules) ? rules : [];
  return GROUPS.map((g) => ({
    key: g.key,
    title: g.title,
    rules: list.map((rule, index) => ({ rule, index })).filter((x) => g.pick(x.rule)),
  })).filter((g) => g.rules.length > 0);
}

export const hasEditRule = (rules) => (rules ?? []).some((r) => r.kind === "edit_outside_layer");

function freshId(rules, base) {
  const taken = new Set((rules ?? []).map((r) => r.id));
  if (!taken.has(base)) return base;
  for (let n = 2; ; n++) if (!taken.has(`${base}-${n}`)) return `${base}-${n}`;
}

export function newEditRule(rules) {
  return {
    id: freshId(rules, "stay-in-your-layer"),
    kind: "edit_outside_layer",
    mode: "blocking",
    message: "You may only change files in the layers your agent owns. Ask the architect.",
  };
}

export function newCommandRule(rules) {
  return { id: freshId(rules, "check"), kind: "command", at: "before_commit", command: "", mode: "warning", message: "" };
}

/** "a, b" as ["a", "b"]; blanks dropped. */
export const splitList = (s) =>
  String(s ?? "")
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);

export const joinList = (xs) => (Array.isArray(xs) ? xs.join(", ") : "");

/** How a rule's firing ends, in words. */
export function outcome(rule) {
  const blocking = rule.mode === "blocking";
  if (rule.kind === "edit_outside_layer") {
    return blocking
      ? "Before a tool call the call is denied; at commit the commit fails and git prints this with the offending paths."
      : "The call goes through and the agent gets this as context; at commit it is printed and the commit goes through.";
  }
  switch (rule.at) {
    case "after_edit":
      return "The edit has already landed, so the agent gets this as context in both modes.";
    case "before_commit":
      return blocking ? "The commit fails." : "It is printed and the commit goes on.";
    case "before_tool":
      return blocking ? "The call is denied." : "The call goes through and the agent gets this as context.";
    default:
      return "";
  }
}

/**
 * What the agent sees. Sample path, layer and agent come from the draft: the
 * first layer's folder, that layer's name and the rule's first agent.
 */
export function previewText(rule, layers) {
  const layer = (layers ?? [])[0];
  const folder = layer?.match?.folder ? String(layer.match.folder).replace(/\/$/, "") : "";
  const path = folder ? `${folder}/example.go` : "src/example.go";
  const agent = rule.agents?.[0] ?? "plain";
  const head = `Architecture rule ${rule.id} (${rule.mode}): ${String(rule.message ?? "").trim() || "(message missing)"}`;
  if (rule.kind === "edit_outside_layer") {
    return `${head} — ${path} is in layer ${layer?.name ?? "unlayered"}, which ${agent} may not change.`;
  }
  let text = `${head}\nexit status 1 (sample output of ${rule.command || "the command"})`;
  if (rule.at === "after_edit" && rule.mode === "blocking") text += "\nUndo or fix this change before you go on.";
  return text;
}

export const NAME_REQUIRED = "Give the rule a name.";
export const MESSAGE_REQUIRED = "A message is required. Say what was refused and what to do instead.";

const blank = (v) => String(v ?? "").trim() === "";

/** What a rule's list row and editor header call it. */
export const ruleTitle = (rule) => (blank(rule?.id) ? "New rule" : String(rule.id).trim());

/** The line under a rule's name in the list: what it does. */
export function ruleSummary(rule) {
  if (rule?.kind === "edit_outside_layer") return "Edits outside the layers the agent may change";
  return blank(rule?.command) ? "No command yet" : String(rule.command).trim();
}

/** The lifecycle point's words; the edit-outside-layer rule has none of its own. */
export function triggerLabel(rule) {
  if (rule?.kind === "edit_outside_layer") return "Before a tool call and at commit";
  return LIFECYCLE_POINTS.find((p) => p.value === rule?.at)?.label ?? "No lifecycle point yet";
}

const COMMAND_ONLY = ["at", "command", "paths", "tools", "timeout_seconds"];

/** The rule as the other kind: what both kinds hold is kept, what only the old kind holds is dropped. */
export function withKind(rule, kind) {
  const next = { ...rule, kind };
  if (kind === "edit_outside_layer") {
    for (const k of COMMAND_ONLY) delete next[k];
    return next;
  }
  return { ...next, at: rule.at ?? "before_commit", command: rule.command ?? "" };
}

/** A copy of a rule under a fresh id. */
export function duplicateRule(rules, rule) {
  // the draft's rules are reactive proxies, which structuredClone refuses
  return { ...JSON.parse(JSON.stringify(rule)), id: freshId(rules, `${rule.id}-copy`) };
}

/**
 * The words for a problem the server found on a rule's field: an empty name or
 * message is said as the design says it, anything else keeps the server's text.
 */
export function issueText(rule, field, serverMessage) {
  if (field === "id" && blank(rule?.id)) return NAME_REQUIRED;
  if (field === "message" && blank(rule?.message)) return MESSAGE_REQUIRED;
  return serverMessage;
}

/** The preview's title bar: whose session and when the rule runs. */
export const previewBar = (rule) => `agent: ${rule?.agents?.[0] ?? "plain"} · ${triggerLabel(rule)}`;
