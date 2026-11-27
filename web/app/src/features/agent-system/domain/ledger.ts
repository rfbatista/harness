// The ledger's rows: each field with its control, its source and its action (UX study, section 2).

import { gatePath, gateRequired, OPTIONAL_GATE_KINDS, REQUIRED_GATE_KINDS, type Field, type FieldError, type Gate, type LastScore, type ModelChoice, type Patch, type Scope, type SettingsView, type Source, type VisualQaUi } from "./settings";
import { globProblem, type Rule } from "./validation";

export type SourceKind = "harness" | "project" | "override" | "missing";

/** Where a row's value comes from, as the page words it. */
export interface SourceMark {
  readonly kind: SourceKind;
  readonly text: string;
  /** The detection reason under a harness default. */
  readonly reason: string;
  /** For an override: what it replaces ("project: off"). */
  readonly replaces: string;
}

/** Override on an inherited row (repository scope), Reset on a value this level sets, nothing on a harness default in project scope. */
export type RowAction = "override" | "reset" | "none";

export type RowKind = "toggle" | "number" | "chips" | "fixed" | "select" | "visualqa";

export interface Row {
  readonly path: string;
  readonly label: string;
  readonly hint: string;
  readonly kind: RowKind;
  /** A toggle's state. */
  readonly value: boolean;
  /** What the row holds now, draft included: a boolean, a number (or what was typed instead), a list. */
  readonly current: unknown;
  /** The chips of a list row. */
  readonly items: readonly string[];
  /** The chips the form finds malformed (a glob with an unclosed bracket). */
  readonly invalid: readonly string[];
  /** A select's options: the inherited one first (value ""), then what the harness offers. */
  readonly options: readonly { readonly value: string; readonly label: string }[];
  /** The option a select shows: "" while the field is inherited from the harness. */
  readonly selected: string;
  /** What the add field of a list row says. */
  readonly placeholder: string;
  /** A toggle's words, off then on. */
  readonly words: readonly [string, string];
  /** A word after a number's input. */
  readonly unit: string;
  /** The text of a fixed row, which the person cannot change. */
  readonly text: string;
  readonly source: SourceMark;
  readonly action: RowAction;
  /** What the form checks before a save; undefined on a fixed row. */
  readonly rule: Rule | undefined;
  /** The person changed this row and has not saved it. */
  readonly draft: boolean;
  /** Why this row cannot be saved, from the form or from a refused save. */
  readonly error: FieldError | undefined;
}

/** One gate kind in the Gates list: its command and timeout (and, for an optional kind, its switch). */
export interface GateRow {
  readonly kind: string;
  readonly label: string;
  readonly required: boolean;
  /** An optional kind's switch; always true on a required kind (it has no switch). */
  readonly on: boolean;
  /** The command as shown, draft included. */
  readonly command: string;
  /** Where the field is empty: what the stack would suggest, or how to leave it. */
  readonly placeholder: string;
  /** The timeout in seconds as shown (or what was typed instead of a number). */
  readonly timeout: number | string;
  /** A required kind with no command at any level. */
  readonly missing: boolean;
  readonly source: SourceMark;
  readonly action: RowAction;
  readonly paths: { readonly command: string; readonly timeout: string; readonly enabled: string };
  readonly draft: boolean;
  readonly errors: { readonly command: FieldError | undefined; readonly timeout: FieldError | undefined; readonly enabled: FieldError | undefined };
}

/** The visual QA UI declaration: one block per repository, added and removed as a whole. */
export interface VisualQaBlock {
  readonly path: string;
  /** Some level declares a UI (or the person drafted one). */
  readonly declared: boolean;
  /** A repository without one may declare it; the project holds no default. */
  readonly canDeclare: boolean;
  /** The declaration as it reads now, draft included; empty when none. */
  readonly ui: VisualQaUi;
  readonly source: SourceMark;
  readonly action: RowAction;
  readonly draft: boolean;
  readonly error: FieldError | undefined;
}

/** A line to another page under a layer's rows: the Score page with the last run. */
export interface LayerLink {
  readonly label: string;
  readonly text: string;
  readonly href: string;
}

/** What the layers show that the settings store does not hold. */
export interface LayerContext {
  readonly models: readonly ModelChoice[];
  readonly lastScore: LastScore | undefined;
  readonly scoreHref: string;
}

export const NO_CONTEXT: LayerContext = { models: [], lastScore: undefined, scoreHref: "" };

export type LayerKey = "learn" | "guard" | "review" | "score";

export interface Layer {
  readonly key: LayerKey;
  readonly title: string;
  readonly purpose: string;
  /** The Gates sub-list of the Guard layer; empty elsewhere. */
  readonly gates: readonly GateRow[];
  readonly rows: readonly Row[];
  /** A line under the rows: the skills of a repository, the last score. */
  readonly note: string;
  /** The visual QA block of the Review layer. */
  readonly visualQa: VisualQaBlock | undefined;
  /** The Score layer's line to the Score page. */
  readonly link: LayerLink | undefined;
  /** What needs the person in this layer: the missing gates of a repository, the red repositories of the project. */
  readonly attention: number;
  /** How many of this layer's fields the shown repository overrides; 0 at project scope. */
  readonly overrides: number;
}

/** The drafts and errors the page holds, by field path. */
export interface Edits {
  readonly draft: Patch;
  readonly errors: Readonly<Record<string, FieldError>>;
}

export const NO_EDITS: Edits = { draft: {}, errors: {} };

/** What a row names: a path, its rule, the value the level holds for it, for the form to check and to compare drafts with. */
export interface Editable {
  readonly path: string;
  readonly section: string;
  readonly label: string;
  readonly rule: Rule;
  readonly current: unknown;
}

const HARNESS: SourceMark = { kind: "harness", text: "harness default", reason: "", replaces: "" };
const PROJECT: SourceMark = { kind: "project", text: "project", reason: "", replaces: "" };

interface FieldSpec<T> {
  readonly path: string;
  readonly label: string;
  readonly hint: string;
  readonly kind: "toggle" | "number" | "chips" | "select" | "visualqa";
  readonly rule: Rule;
  readonly harnessDefault: T;
  readonly field: (view: SettingsView) => Field<T>;
  /** How the value reads in "project: …" and "harness default: …". */
  readonly show: (value: T) => string;
  readonly words?: readonly [string, string];
  readonly unit?: string;
  readonly placeholder?: string;
  readonly options?: readonly { readonly value: string; readonly label: string }[];
}

const showBool = (on: boolean): string => (on ? "on" : "off");
const showNumber = (n: number): string => String(n);
const showList = (items: readonly string[]): string => (items.length <= 2 ? items.join(", ") : `${items.length} entries`);

/** The source mark of one field at `scope`, with its action. `project` is the project level's own view when the scope is a repository. */
function sourceOf<T>(scope: Scope, spec: FieldSpec<T>, field: Field<T>, project: SettingsView | undefined): { mark: SourceMark; action: RowAction } {
  const from: Source = field.source;
  if (from === "repository") {
    const projectValue = project?.values[spec.path];
    const replaced = project !== undefined && spec.path in project.values ? `project: ${spec.show(projectValue as T)}` : `harness default: ${spec.show(spec.harnessDefault)}`;
    return { mark: { kind: "override", text: "override", reason: "", replaces: replaced }, action: "reset" };
  }
  if (from === "project") return { mark: PROJECT, action: scope.kind === "repository" ? "override" : "reset" };
  return { mark: { ...HARNESS, reason: field.reason }, action: scope.kind === "repository" ? "override" : "none" };
}

/** The value a field falls back to when this level does not set it: the project's, else the harness default. */
function inherited<T>(scope: Scope, spec: FieldSpec<T>, project: SettingsView | undefined): { value: T; mark: SourceMark } {
  if (scope.kind === "repository" && project !== undefined && spec.path in project.values) return { value: project.values[spec.path] as T, mark: PROJECT };
  return { value: spec.harnessDefault, mark: HARNESS };
}

function fieldRow<T>(scope: Scope, view: SettingsView, project: SettingsView | undefined, spec: FieldSpec<T>, edits: Edits): Row {
  const row = plainRow(scope, view, project, spec, edits);
  // A select shows the inherited option while the harness decides, and the model itself once a level sets it.
  return spec.kind === "select" ? { ...row, selected: row.source.kind === "harness" ? "" : String(row.current) } : row;
}

function plainRow<T>(scope: Scope, view: SettingsView, project: SettingsView | undefined, spec: FieldSpec<T>, edits: Edits): Row {
  const common = {
    path: spec.path,
    label: spec.label,
    hint: spec.hint,
    kind: spec.kind,
    text: "",
    rule: spec.rule,
    words: spec.words ?? (["Off", "On"] as const),
    unit: spec.unit ?? "",
    placeholder: spec.placeholder ?? "",
    options: spec.options ?? [],
    selected: "",
    error: edits.errors[spec.path],
  };
  const shaped = (current: unknown) => ({
    value: current === true,
    current,
    items: Array.isArray(current) ? (current as string[]) : [],
    invalid: spec.rule.kind === "globs" && Array.isArray(current) ? (current as string[]).filter((g) => globProblem(g) !== undefined) : [],
  });
  if (spec.path in edits.draft) {
    const draft = edits.draft[spec.path];
    const below = inherited(scope, spec, project);
    // A reset reads as what the level below gives; a value reads as this level's own, as it will after the save.
    if (draft === null) return { ...common, ...shaped(below.value), source: below.mark, action: scope.kind === "repository" ? "override" : "none", draft: true };
    const replaces = below.mark.kind === "project" ? `project: ${spec.show(below.value)}` : `harness default: ${spec.show(below.value)}`;
    const mark: SourceMark = scope.kind === "repository" ? { kind: "override", text: "override", reason: "", replaces } : PROJECT;
    return { ...common, ...shaped(draft), source: mark, action: "reset", draft: true };
  }
  const field = spec.field(view);
  const { mark, action } = sourceOf(scope, spec, field, project);
  return { ...common, ...shaped(field.value), source: mark, action, draft: false };
}

const LEARN_TOGGLES: readonly FieldSpec<boolean>[] = [
  { path: "learn.context_map.enabled", label: "Context map", hint: "Generated from the default branch, stamped with its commit, delivered beside AGENTS.md.", kind: "toggle", rule: { kind: "bool" }, harnessDefault: true, field: (v) => v.effective.contextMapEnabled, show: showBool },
  { path: "learn.task_memory.require_update", label: "Task memory must be updated", hint: "A done declaration with an unchanged progress.md is asked once to update it; the omission is recorded.", kind: "toggle", rule: { kind: "bool" }, harnessDefault: true, field: (v) => v.effective.taskMemoryRequireUpdate, show: showBool },
];

/** The harness default protected paths (the contract's): the gate scripts and the CI configuration. */
const DEFAULT_PROTECTED_PATHS: readonly string[] = ["check.sh", ".github/workflows/**", ".gitlab-ci.yml", ".circleci/**", ".buildkite/**", "Jenkinsfile", "azure-pipelines.yml"];

export const RETRY_LIMIT_RANGE = { min: 1, max: 10 } as const;
export const GATE_TIMEOUT_RANGE = { min: 1, max: 86_400 } as const;
const DEFAULT_GATE_TIMEOUT = 600;

const PROTECTED_PATHS: FieldSpec<readonly string[]> = { path: "guard.protected_paths", label: "Protected paths", hint: "Globs a gated session may not write unless its scope unlocks them.", kind: "chips", rule: { kind: "globs" }, harnessDefault: DEFAULT_PROTECTED_PATHS, field: (v) => v.effective.guard.protectedPaths, show: showList };
const LOCKFILE_RULE: FieldSpec<boolean> = { path: "guard.lockfile_rule.enabled", label: "Lockfile rule", hint: "A lockfile changes only with its manifest; new dependencies are flagged on the session.", kind: "toggle", rule: { kind: "bool" }, harnessDefault: true, field: (v) => v.effective.guard.lockfileRuleEnabled, show: showBool };
const RETRY_LIMIT: FieldSpec<number> = { path: "guard.retry_limit", label: "Retry limit", hint: "Consecutive red gate runs before the session is escalated. 1 to 10.", kind: "number", rule: { kind: "int", ...RETRY_LIMIT_RANGE }, harnessDefault: 3, field: (v) => v.effective.guard.retryLimit, show: showNumber, unit: "red runs in a row" };
const TESTS_FAIL_FIRST: FieldSpec<boolean> = { path: "guard.tests_fail_first.available", label: "Tests fail first", hint: "Available to workflow templates. Off: no template can turn it on.", kind: "toggle", rule: { kind: "bool" }, harnessDefault: false, field: (v) => v.effective.guard.testsFailFirstAvailable, show: (on) => (on ? "available" : "not available"), words: ["Not available", "Available"] };

const GATE_LABELS: Readonly<Record<string, string>> = { build: "Build", test: "Test", lint: "Lint", secret_scan: "Secret scan", lockfile: "Lockfile", typecheck: "Type check", sast: "SAST", dependency_scan: "Dependency scan" };
export const gateLabel = (kind: string): string => GATE_LABELS[kind] ?? kind;

/** What an empty optional command suggests (the mockup's own placeholders). */
const SUGGESTIONS: Readonly<Record<string, string>> = { typecheck: "not set for this stack", sast: "e.g. gosec ./...", dependency_scan: "e.g. govulncheck ./..." };

const FIXED: Pick<Row, "text" | "words" | "unit" | "items" | "invalid" | "options" | "selected" | "placeholder"> = { text: "", words: ["Off", "On"], unit: "", items: [], invalid: [], options: [], selected: "", placeholder: "" };

function gateAttribute(kind: string): { command: string; timeout: string; enabled: string } {
  return { command: gatePath(kind, "command"), timeout: gatePath(kind, "timeout_seconds"), enabled: gatePath(kind, "enabled") };
}

/** One gate kind as a row, at `scope`, with the person's draft over what the level holds. */
function gateRow(scope: Scope, view: SettingsView, project: SettingsView | undefined, gate: Gate, edits: Edits): GateRow {
  const paths = gateAttribute(gate.kind);
  const required = gateRequired(gate.kind);
  const set = (path: string): boolean => (path in edits.draft ? edits.draft[path] !== null : path in view.values);
  const levelSets = set(paths.command) || set(paths.timeout) || set(paths.enabled);
  const projectValue = (path: string): unknown => (scope.kind === "repository" && project !== undefined ? project.values[path] : undefined);

  // What the level below gives, when the person is resetting this gate here.
  const resetting = [paths.command, paths.timeout, paths.enabled].some((p) => p in edits.draft && edits.draft[p] === null) && !levelSets;
  const belowCommand = projectValue(paths.command);

  let command: string;
  if (paths.command in edits.draft) {
    const draft = edits.draft[paths.command];
    command = draft === null ? (typeof belowCommand === "string" ? belowCommand : "") : String(draft);
  } else if (!gate.on && typeof (view.values[paths.command] ?? projectValue(paths.command)) === "string") {
    command = String(view.values[paths.command] ?? projectValue(paths.command));
  } else {
    command = gate.command.value;
  }

  let timeout: number | string = gate.timeoutSeconds.value;
  if (paths.timeout in edits.draft) {
    const draft = edits.draft[paths.timeout];
    const below = projectValue(paths.timeout);
    timeout = draft === null ? (typeof below === "number" ? below : DEFAULT_GATE_TIMEOUT) : typeof draft === "number" ? draft : String(draft);
  } else if (!gate.on && typeof (view.values[paths.timeout] ?? projectValue(paths.timeout)) === "number") {
    timeout = (view.values[paths.timeout] ?? projectValue(paths.timeout)) as number;
  }

  let on = required || gate.on;
  if (!required && paths.enabled in edits.draft) {
    const draft = edits.draft[paths.enabled];
    on = draft === null ? command !== "" : draft === true;
  } else if (!required && !gate.on && typeof edits.draft[paths.command] === "string" && String(edits.draft[paths.command]).trim() !== "" && view.values[paths.enabled] !== false && projectValue(paths.enabled) !== false) {
    // A command typed or imported into an optional kind turns it on: the store's `enabled` defaults to true.
    on = true;
  }

  const missing = required && command.trim() === "" && !(paths.command in edits.draft && typeof edits.draft[paths.command] === "string" && String(edits.draft[paths.command]).trim() !== "");

  let source: SourceMark;
  let action: RowAction;
  if (levelSets) {
    const replaced = typeof belowCommand === "string" ? `project: ${belowCommand}` : "harness default";
    source = scope.kind === "repository" ? { kind: "override", text: "override", reason: "", replaces: replaced } : PROJECT;
    action = "reset";
  } else if (resetting) {
    source = typeof belowCommand === "string" ? PROJECT : HARNESS;
    action = scope.kind === "repository" ? "override" : "none";
  } else if (missing) {
    source = { kind: "missing", text: "no command at any level", reason: "", replaces: "" };
    action = "none";
  } else if (gate.command.source === "project") {
    source = PROJECT;
    action = scope.kind === "repository" ? "override" : "reset";
  } else {
    source = { ...HARNESS, reason: gate.command.reason };
    action = scope.kind === "repository" && (required || on) ? "override" : "none";
  }

  const drafted = [paths.command, paths.timeout, paths.enabled].some((p) => p in edits.draft);
  const placeholder = required ? "the command a session runs" : (SUGGESTIONS[gate.kind] ?? "");
  const errors = { command: edits.errors[paths.command], timeout: edits.errors[paths.timeout], enabled: edits.errors[paths.enabled] };
  return { kind: gate.kind, label: gateLabel(gate.kind), required, on, command, placeholder, timeout, missing, source, action, paths, draft: drafted, errors };
}

/** The Learn layer: context map, its regeneration rule, task memory, and (in a repository) the skills line. */
export function learnLayer(scope: Scope, view: SettingsView, project: SettingsView | undefined, edits: Edits = NO_EDITS): Layer {
  const [contextMap, taskMemory] = LEARN_TOGGLES.map((spec) => fieldRow(scope, view, project, spec, edits)) as [Row, Row];
  const regenerate: Row = { ...FIXED, path: "", label: "Regenerate", hint: "When the default branch moves. Not configurable in v1.", kind: "fixed", value: true, current: true, text: "on every default-branch commit", source: HARNESS, action: "none", rule: undefined, draft: false, error: undefined };
  return {
    key: "learn",
    title: "Learn",
    purpose: "What enters a session's context.",
    gates: [],
    visualQa: undefined,
    link: undefined,
    rows: [contextMap, regenerate, taskMemory],
    note: scope.kind === "repository" ? "Skills are read from this repository's instruction files." : "Choose a repository to see its skills and activation rules.",
    attention: 0,
    overrides: 0,
  };
}

/** The Guard layer: the Gates sub-list, then protected paths, the lockfile rule, the retry limit, tests fail first. */
export function guardLayer(scope: Scope, view: SettingsView, project: SettingsView | undefined, edits: Edits = NO_EDITS): Layer {
  const row = <T,>(spec: FieldSpec<T>): Row => fieldRow(scope, view, project, spec, edits);
  const kinds = [...REQUIRED_GATE_KINDS, ...OPTIONAL_GATE_KINDS] as readonly string[];
  const byKind = new Map(view.effective.guard.gates.map((g) => [g.kind, g] as const));
  const gates = kinds.map((kind) => {
    const gate = byKind.get(kind) ?? { kind, required: gateRequired(kind), on: false, command: { value: "", source: "harness" as const, reason: "" }, timeoutSeconds: { value: DEFAULT_GATE_TIMEOUT, source: "harness" as const, reason: "" } };
    return gateRow(scope, view, project, gate, edits);
  });
  return {
    key: "guard",
    title: "Guard",
    purpose: "Deterministic checks the agent cannot skip.",
    gates,
    visualQa: undefined,
    link: undefined,
    rows: [row(PROTECTED_PATHS), row(LOCKFILE_RULE), row(RETRY_LIMIT), row(TESTS_FAIL_FIRST)],
    note: "",
    attention: scope.kind === "repository" ? gates.filter((g) => g.missing).length : 0,
    overrides: 0,
  };
}

/** The model the review-mode sessions use, when a level that is not set by the person tells it: the critic model's harness default. */
function reviewModelName(view: SettingsView, project: SettingsView | undefined): string {
  for (const v of [view, project]) if (v !== undefined && v.effective.critic.model.source === "harness") return v.effective.critic.model.value;
  return "";
}

/** The seed of a new declaration: the repository's first run command, the usual local URL, the front page at two sizes. */
export const NO_VISUAL_QA: VisualQaUi = { start_command: "", url: "", routes: [], viewports: [], ui_paths: [] };

const DEFAULT_VIEWPORTS = ["1440x900", "390x844"];

/** The declaration a person starts from. */
export function newVisualQa(startCommand: string): VisualQaUi {
  return { start_command: startCommand, url: "http://127.0.0.1:8080", routes: ["/"], viewports: DEFAULT_VIEWPORTS, ui_paths: [] };
}

/** The Review layer: the critic (on, model, standards) and the visual QA UI declaration. */
export function reviewLayer(scope: Scope, view: SettingsView, project: SettingsView | undefined, edits: Edits = NO_EDITS, ctx: LayerContext = NO_CONTEXT): Layer {
  const reviewModel = reviewModelName(view, project);
  const enabled: FieldSpec<boolean> = { path: "review.critic.enabled", label: "Launch the critic", hint: "Launched by the harness after every green gate run of a gated session. Advisory: never refuses a done declaration.", kind: "toggle", rule: { kind: "bool" }, harnessDefault: true, field: (v) => v.effective.critic.enabled, show: showBool };
  const model: FieldSpec<string> = {
    path: "review.critic.model",
    label: "Critic model",
    hint: "May differ from the coder's model.",
    kind: "select",
    rule: { kind: "model", options: ctx.models.map((m) => m.value) },
    harnessDefault: reviewModel,
    field: (v) => v.effective.critic.model,
    show: (m) => m,
    options: [{ value: "", label: reviewModel === "" ? "Review-mode model" : `Review-mode model (${reviewModel})` }, ...ctx.models],
  };
  const standards: FieldSpec<readonly string[]> = { path: "review.critic.standards", label: "Standards the critic reads", hint: "Paths and sources the critic holds the diff to.", kind: "chips", rule: { kind: "globs" }, harnessDefault: DEFAULT_CRITIC_STANDARDS, field: (v) => v.effective.critic.standards, show: showList, placeholder: "add a path" };
  const ui: FieldSpec<VisualQaUi | null> = { path: "review.visual_qa.ui", label: "UI declaration", hint: "", kind: "visualqa", rule: { kind: "visual-qa" }, harnessDefault: null, field: (v) => v.effective.visualQaUi, show: (u) => (u === null ? "none" : "declared") };
  const declaration = fieldRow(scope, view, project, ui, edits);
  const current = declaration.current as VisualQaUi | null;
  const visualQa: VisualQaBlock = {
    path: ui.path,
    declared: current !== null && current !== undefined,
    canDeclare: scope.kind === "repository" && (current === null || current === undefined),
    ui: current ?? NO_VISUAL_QA,
    source: declaration.source,
    action: declaration.action,
    draft: declaration.draft,
    error: declaration.error,
  };
  return {
    key: "review",
    title: "Review",
    purpose: "A second look at a session's work before the person's.",
    gates: [],
    visualQa,
    link: undefined,
    rows: [fieldRow(scope, view, project, enabled, edits), fieldRow(scope, view, project, model, edits), fieldRow(scope, view, project, standards, edits)],
    note: "",
    attention: 0,
    overrides: 0,
  };
}

const DEFAULT_CRITIC_STANDARDS: readonly string[] = ["AGENTS.md", "CLAUDE.md", "context map"];

/** "Score page · 24 cases · last run 0.82 · $1.14": the last finished bench run, or a dash. */
function scoreLine(last: LastScore | undefined): string {
  if (last === undefined) return "Score page · last run —";
  const cases = last.cases > 0 ? ` · ${last.cases} ${last.cases === 1 ? "case" : "cases"}` : "";
  return `Score page${cases} · last run ${last.mean.toFixed(2)} · $${last.costUsd.toFixed(2)}`;
}

/** The Score layer: trajectory recording for new tasks, and the line to the Score page. */
export function scoreLayer(scope: Scope, view: SettingsView, project: SettingsView | undefined, edits: Edits = NO_EDITS, ctx: LayerContext = NO_CONTEXT): Layer {
  const spec: FieldSpec<boolean> = { path: "score.trajectory_recording.default", label: "Trajectory recording", hint: "Record the tool calls of new tasks' sessions, to turn them into bench cases. A task can override it.", kind: "toggle", rule: { kind: "bool" }, harnessDefault: false, field: (v) => v.effective.trajectoryRecordingDefault, show: showBool };
  const link: LayerLink | undefined = ctx.scoreHref === "" ? undefined : { label: "Bench", text: scoreLine(ctx.lastScore), href: ctx.scoreHref };
  return { key: "score", title: "Score", purpose: "The offline bench that measures this project's agent setup.", gates: [], visualQa: undefined, link, rows: [fieldRow(scope, view, project, spec, edits)], note: "", attention: 0, overrides: 0 };
}

/** The layers, in the study's order. */
export function layers(scope: Scope, view: SettingsView, project: SettingsView | undefined, edits: Edits = NO_EDITS, ctx: LayerContext = NO_CONTEXT): readonly Layer[] {
  return [learnLayer(scope, view, project, edits), guardLayer(scope, view, project, edits), reviewLayer(scope, view, project, edits, ctx), scoreLayer(scope, view, project, edits, ctx)];
}

/** Every path a layer edits, with its rule and what it holds now: what the form checks a draft against. */
export function editables(shown: readonly Layer[]): readonly Editable[] {
  const out: Editable[] = [];
  for (const layer of shown) {
    for (const row of layer.rows) if (row.rule !== undefined && row.path !== "") out.push({ path: row.path, section: layer.title, label: row.label, rule: row.rule, current: row.current });
    if (layer.visualQa !== undefined) out.push({ path: layer.visualQa.path, section: layer.title, label: "UI declaration", rule: { kind: "visual-qa" }, current: layer.visualQa.declared ? layer.visualQa.ui : null });
    for (const g of layer.gates) {
      out.push({ path: g.paths.command, section: layer.title, label: `${g.label} command`, rule: g.required ? { kind: "required-command" } : { kind: "command" }, current: g.command });
      out.push({ path: g.paths.timeout, section: layer.title, label: `${g.label} timeout`, rule: { kind: "int", ...GATE_TIMEOUT_RANGE }, current: g.timeout });
      if (!g.required) out.push({ path: g.paths.enabled, section: layer.title, label: g.label, rule: { kind: "bool" }, current: g.on });
    }
  }
  return out;
}
