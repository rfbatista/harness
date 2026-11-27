// The agent system settings as the page reads them (Contract: Agent system settings store ↔ layers and
// web UI). Every effective value carries the level it came from; what a level sets itself is `values`.

/** The level an effective value came from. */
export type Source = "repository" | "project" | "harness";

export interface Field<T> {
  readonly value: T;
  readonly source: Source;
  /** Why, for a harness default that was derived ("detected from go.mod"). */
  readonly reason: string;
}

/** One gate kind as a session runs it. An optional kind the level leaves off is absent from the store's list: `on` is false. */
export interface Gate {
  readonly kind: string;
  readonly required: boolean;
  readonly on: boolean;
  readonly command: Field<string>;
  readonly timeoutSeconds: Field<number>;
}

export interface Guard {
  readonly gates: readonly Gate[];
  readonly protectedPaths: Field<readonly string[]>;
  readonly lockfileRuleEnabled: Field<boolean>;
  readonly retryLimit: Field<number>;
  readonly testsFailFirstAvailable: Field<boolean>;
}

/** The UI a repository declares for visual QA, as the store keeps it (wire names). */
export interface VisualQaUi {
  readonly start_command: string;
  readonly url: string;
  readonly routes: readonly string[];
  /** "WIDTHxHEIGHT", e.g. 1440x900. */
  readonly viewports: readonly string[];
  readonly ui_paths: readonly string[];
}

export interface Critic {
  readonly enabled: Field<boolean>;
  /** A model the harness can call; at the harness default it is the review-mode session model. */
  readonly model: Field<string>;
  readonly standards: Field<readonly string[]>;
}

export interface Effective {
  readonly contextMapEnabled: Field<boolean>;
  readonly taskMemoryRequireUpdate: Field<boolean>;
  readonly guard: Guard;
  readonly critic: Critic;
  /** The UI declaration; null where no level declares one. */
  readonly visualQaUi: Field<VisualQaUi | null>;
  readonly trajectoryRecordingDefault: Field<boolean>;
  /** The required gate kinds with no command at any level: the repository is red. */
  readonly missingRequiredGates: readonly string[];
}

/** One level as the page reads it: the effective configuration, and what the level sets itself by field path. */
export interface SettingsView {
  readonly effective: Effective;
  readonly values: Readonly<Record<string, unknown>>;
}

/** The gate kinds, in the order the Gates list shows them (Glossary "Gate"). */
export const REQUIRED_GATE_KINDS = ["build", "test", "lint", "secret_scan", "lockfile"] as const;
export const OPTIONAL_GATE_KINDS = ["typecheck", "sast", "dependency_scan"] as const;

export const gateRequired = (kind: string): boolean => (REQUIRED_GATE_KINDS as readonly string[]).includes(kind);

/** The field path of one attribute of a gate: guard.gates.<kind>.<attribute>. */
export const gatePath = (kind: string, attribute: "command" | "timeout_seconds" | "enabled"): string => `guard.gates.${kind}.${attribute}`;

/** A model the select offers: its value and its words. */
export interface ModelChoice {
  readonly value: string;
  readonly label: string;
}

/** A run command of a repository: a start command the UI declaration may name. */
export interface RunCommand {
  readonly name: string;
  readonly command: string;
}

/** A gate command a repository's check.sh fills, as the import preview answers it. */
export interface ImportedGate {
  readonly kind: string;
  readonly command: string;
  readonly reason: string;
}

/** The last finished bench run of the project, for the Score section's line. */
export interface LastScore {
  readonly mean: number;
  readonly costUsd: number;
  readonly cases: number;
}

export interface Repository {
  readonly id: string;
  readonly name: string;
}

/** Project defaults, or one repository's overrides over them. */
export type Scope = { readonly kind: "project" } | { readonly kind: "repository"; readonly id: string };

export const PROJECT_SCOPE: Scope = { kind: "project" };

export const sameScope = (a: Scope, b: Scope): boolean => a.kind === b.kind && (a.kind === "project" || (b.kind === "repository" && a.id === b.id));

/** A save: field path → new value, or null to reset the field at that level. */
export type Patch = Readonly<Record<string, unknown>>;

export interface FieldError {
  readonly field: string;
  readonly code: string;
  readonly message: string;
}

/** The store refused a save (422): its errors name the same field paths. */
export class SettingsRefused extends Error {
  readonly errors: readonly FieldError[];
  constructor(errors: readonly FieldError[]) {
    super(errors.map((e) => e.message).join("; ") || "the settings were refused");
    this.name = "SettingsRefused";
    this.errors = errors;
  }
}

export type SettingsErrorCode = "NOT_FOUND" | "NETWORK_ERROR" | "BAD_RESPONSE" | "UNKNOWN";

/** Any other failure of the store, with a code the page can word. */
export class SettingsError extends Error {
  readonly code: SettingsErrorCode;
  constructor(code: SettingsErrorCode, message: string) {
    super(message);
    this.name = "SettingsError";
    this.code = code;
  }
}

/** A repository is red when a required gate has no command at any level. */
export const isRed = (view: SettingsView): boolean => view.effective.missingRequiredGates.length > 0;

/** Structural equality of two saved values (booleans, numbers, text, lists, objects). */
export function sameValue(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((v, i) => sameValue(v, b[i]));
  if (typeof a === "object" && typeof b === "object" && a !== null && b !== null && !Array.isArray(a) && !Array.isArray(b)) {
    const x = a as Record<string, unknown>;
    const y = b as Record<string, unknown>;
    const keys = Object.keys(x);
    return keys.length === Object.keys(y).length && keys.every((k) => k in y && sameValue(x[k], y[k]));
  }
  return false;
}
