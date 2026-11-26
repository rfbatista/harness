// Row validation before save: what the form can check itself. The store checks the rest (globs, routes,
// models) and answers 422; those errors land on the same row.

import type { FieldError } from "./settings";

/** What a field takes. Bool rows are switches; the others arrive with the sections that edit them. */
export type Rule =
  | { readonly kind: "bool" }
  | { readonly kind: "int"; readonly min: number; readonly max: number }
  | { readonly kind: "text" }
  /** A required gate's command: emptying it is refused; the way back to a value is Reset. */
  | { readonly kind: "required-command" }
  | { readonly kind: "list" };

/** The error a draft value has under `rule`, or undefined. `null` is a reset and is always allowed. */
export function check(path: string, rule: Rule, value: unknown): FieldError | undefined {
  if (value === null) return undefined;
  switch (rule.kind) {
    case "bool":
      return typeof value === "boolean" ? undefined : { field: path, code: "INVALID_TYPE", message: "Takes on or off." };
    case "int":
      if (typeof value !== "number" || !Number.isInteger(value)) return { field: path, code: "INVALID_TYPE", message: "Takes a whole number." };
      return value < rule.min || value > rule.max ? { field: path, code: "OUT_OF_RANGE", message: `Takes ${rule.min}–${rule.max}.` } : undefined;
    case "text":
      return typeof value === "string" ? undefined : { field: path, code: "INVALID_TYPE", message: "Takes text." };
    case "required-command":
      if (typeof value !== "string") return { field: path, code: "INVALID_TYPE", message: "Takes text." };
      return value.trim() === "" ? { field: path, code: "REQUIRED_GATE_MISSING", message: "A required gate needs a command. Reset the field to inherit one." } : undefined;
    case "list":
      return Array.isArray(value) && value.every((v) => typeof v === "string" && v.trim() !== "") ? undefined : { field: path, code: "INVALID_TYPE", message: "Takes a list of non-empty entries." };
  }
}
