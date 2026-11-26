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

export interface Effective {
  readonly contextMapEnabled: Field<boolean>;
  readonly taskMemoryRequireUpdate: Field<boolean>;
  readonly trajectoryRecordingDefault: Field<boolean>;
  /** The required gate kinds with no command at any level: the repository is red. */
  readonly missingRequiredGates: readonly string[];
}

/** One level as the page reads it: the effective configuration, and what the level sets itself by field path. */
export interface SettingsView {
  readonly effective: Effective;
  readonly values: Readonly<Record<string, unknown>>;
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
