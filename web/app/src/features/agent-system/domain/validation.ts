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
  /** An optional gate's command: it cannot be emptied either; turn the gate off, or Reset. */
  | { readonly kind: "command" }
  | { readonly kind: "list" }
  /** Globs: a list whose entries must be well formed. */
  | { readonly kind: "globs" };

/** Why a glob is not one the store would take, or undefined: an unclosed bracket or brace. */
export function globProblem(glob: string): string | undefined {
  let square = 0;
  let curly = 0;
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i];
    if (c === "\\") i++;
    else if (c === "[") square++;
    else if (c === "]") square = Math.max(0, square - 1);
    else if (c === "{") curly++;
    else if (c === "}") curly = Math.max(0, curly - 1);
  }
  if (square > 0) return `${glob} has an unclosed bracket. Fix it or remove it to save.`;
  if (curly > 0) return `${glob} has an unclosed brace. Fix it or remove it to save.`;
  return undefined;
}

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
    case "command":
      if (typeof value !== "string") return { field: path, code: "INVALID_TYPE", message: "Takes text." };
      return value.trim() === "" ? { field: path, code: "INVALID_TYPE", message: "A command cannot be empty. Reset the field to inherit it, or turn the gate off." } : undefined;
    case "globs": {
      if (!Array.isArray(value) || !value.every((v) => typeof v === "string" && v.trim() !== "")) return { field: path, code: "INVALID_TYPE", message: "Takes a list of non-empty globs." };
      for (const glob of value as string[]) {
        const problem = globProblem(glob);
        if (problem !== undefined) return { field: path, code: "INVALID_GLOB", message: problem };
      }
      return undefined;
    }
    case "list":
      return Array.isArray(value) && value.every((v) => typeof v === "string" && v.trim() !== "") ? undefined : { field: path, code: "INVALID_TYPE", message: "Takes a list of non-empty entries." };
  }
}
