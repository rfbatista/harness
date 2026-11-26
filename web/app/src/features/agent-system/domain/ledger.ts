// The ledger's rows: each field with its control, its source and its action (UX study, section 2).

import type { Field, Scope, SettingsView, Source } from "./settings";

export type SourceKind = "harness" | "project" | "override";

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

export type RowKind = "toggle" | "fixed";

export interface Row {
  readonly path: string;
  readonly label: string;
  readonly hint: string;
  readonly kind: RowKind;
  readonly value: boolean;
  /** The text of a fixed row, which the person cannot change. */
  readonly text: string;
  readonly source: SourceMark;
  readonly action: RowAction;
}

export type LayerKey = "learn" | "guard" | "review" | "score";

export interface Layer {
  readonly key: LayerKey;
  readonly title: string;
  readonly purpose: string;
  readonly rows: readonly Row[];
  /** A line under the rows: the skills of a repository, the last score. */
  readonly note: string;
}

const word = (on: boolean): string => (on ? "On" : "Off");

/** The source mark of one field at `scope`; `project` is the project level's own view when the scope is a repository. */
export function sourceMark(scope: Scope, field: Field<boolean>, harnessDefault: boolean, project: SettingsView | undefined, path: string): { mark: SourceMark; action: RowAction } {
  const from: Source = field.source;
  if (from === "repository") {
    const projectSets = project !== undefined && path in project.values;
    const replaced = projectSets ? `project: ${word(project.values[path] === true).toLowerCase()}` : `harness default: ${word(harnessDefault).toLowerCase()}`;
    return { mark: { kind: "override", text: "override", reason: "", replaces: replaced }, action: "reset" };
  }
  if (from === "project") {
    return { mark: { kind: "project", text: "project", reason: "", replaces: "" }, action: scope.kind === "repository" ? "override" : "reset" };
  }
  return { mark: { kind: "harness", text: "harness default", reason: field.reason, replaces: "" }, action: scope.kind === "repository" ? "override" : "none" };
}

interface ToggleSpec {
  readonly path: string;
  readonly label: string;
  readonly hint: string;
  readonly harnessDefault: boolean;
  readonly field: (view: SettingsView) => Field<boolean>;
}

const LEARN_TOGGLES: readonly ToggleSpec[] = [
  { path: "learn.context_map.enabled", label: "Context map", hint: "Generated from the default branch, stamped with its commit, delivered beside AGENTS.md.", harnessDefault: true, field: (v) => v.effective.contextMapEnabled },
  { path: "learn.task_memory.require_update", label: "Task memory must be updated", hint: "A done declaration with an unchanged progress.md is asked once to update it; the omission is recorded.", harnessDefault: true, field: (v) => v.effective.taskMemoryRequireUpdate },
];

function toggleRow(scope: Scope, view: SettingsView, project: SettingsView | undefined, spec: ToggleSpec): Row {
  const field = spec.field(view);
  const { mark, action } = sourceMark(scope, field, spec.harnessDefault, project, spec.path);
  return { path: spec.path, label: spec.label, hint: spec.hint, kind: "toggle", value: field.value, text: "", source: mark, action };
}

const HARNESS: SourceMark = { kind: "harness", text: "harness default", reason: "", replaces: "" };

/** The Learn layer: context map, its regeneration rule, task memory, and (in a repository) the skills line. */
export function learnLayer(scope: Scope, view: SettingsView, project: SettingsView | undefined): Layer {
  const [contextMap, taskMemory] = LEARN_TOGGLES.map((spec) => toggleRow(scope, view, project, spec)) as [Row, Row];
  const regenerate: Row = { path: "", label: "Regenerate", hint: "When the default branch moves. Not configurable in v1.", kind: "fixed", value: true, text: "on every default-branch commit", source: HARNESS, action: "none" };
  return {
    key: "learn",
    title: "Learn",
    purpose: "What enters a session's context.",
    rows: [contextMap, regenerate, taskMemory],
    note: scope.kind === "repository" ? "Skills are read from this repository's instruction files." : "Choose a repository to see its skills and activation rules.",
  };
}

/** The Score layer: trajectory recording for new tasks. */
export function scoreLayer(scope: Scope, view: SettingsView, project: SettingsView | undefined): Layer {
  const spec: ToggleSpec = { path: "score.trajectory_recording.default", label: "Trajectory recording", hint: "Record the tool calls of new tasks' sessions, to turn them into bench cases. A task can override it.", harnessDefault: false, field: (v) => v.effective.trajectoryRecordingDefault };
  return { key: "score", title: "Score", purpose: "The offline bench that measures this project's agent setup.", rows: [toggleRow(scope, view, project, spec)], note: "" };
}

/** The layers of this ticket, in the study's order: Learn first, Score last. Guard and Review arrive with their own tickets. */
export function layers(scope: Scope, view: SettingsView, project: SettingsView | undefined): readonly Layer[] {
  return [learnLayer(scope, view, project), scoreLayer(scope, view, project)];
}
