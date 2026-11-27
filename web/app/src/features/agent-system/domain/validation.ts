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
  | { readonly kind: "globs" }
  /** A model the harness can call; with no options known the store decides. */
  | { readonly kind: "model"; readonly options: readonly string[] }
  /** The visual QA UI declaration as a whole: URL, routes, viewports and UI paths. */
  | { readonly kind: "visual-qa" };

/** A viewport is WIDTHxHEIGHT in pixels. */
export const VIEWPORT = /^[1-9]\d*x[1-9]\d*$/;

/** The first thing wrong with a UI declaration, worded for its row, or undefined. */
export function visualQaProblem(ui: unknown): string | undefined {
  if (typeof ui !== "object" || ui === null || Array.isArray(ui)) return "Takes the UI declaration as one block.";
  const v = ui as Record<string, unknown>;
  const url = typeof v["url"] === "string" ? v["url"] : "";
  try {
    const parsed = new URL(url);
    if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || parsed.host === "") throw new Error("scheme");
  } catch {
    return `URL ${url === "" ? "is empty" : `"${url}" is not valid`}: it must be an http or https URL.`;
  }
  const routes = Array.isArray(v["routes"]) ? (v["routes"] as unknown[]) : [];
  for (let i = 0; i < routes.length; i++) {
    const route = routes[i];
    if (typeof route !== "string" || !route.startsWith("/")) return `Route on line ${i + 1}${typeof route === "string" ? ` "${route}"` : ""} must start with /.`;
  }
  const viewports = Array.isArray(v["viewports"]) ? (v["viewports"] as unknown[]) : [];
  for (const viewport of viewports) if (typeof viewport !== "string" || !VIEWPORT.test(viewport)) return `Viewport "${String(viewport)}" must be WIDTHxHEIGHT in pixels, e.g. 1280x800.`;
  const paths = Array.isArray(v["ui_paths"]) ? (v["ui_paths"] as unknown[]) : [];
  for (const glob of paths) {
    const problem = typeof glob === "string" ? globProblem(glob) : "A UI path is a glob.";
    if (problem !== undefined) return problem;
  }
  return undefined;
}

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
    case "model":
      if (typeof value !== "string") return { field: path, code: "INVALID_TYPE", message: "Takes a model name." };
      return rule.options.length === 0 || rule.options.includes(value) ? undefined : { field: path, code: "MODEL_UNAVAILABLE", message: `The harness cannot call "${value}". It offers ${rule.options.join(", ")}.` };
    case "visual-qa": {
      const problem = visualQaProblem(value);
      return problem === undefined ? undefined : { field: path, code: problem.startsWith("Route") || problem.startsWith("URL") || problem.startsWith("Viewport") ? "INVALID_ROUTE" : "INVALID_GLOB", message: problem };
    }
    case "list":
      return Array.isArray(value) && value.every((v) => typeof v === "string" && v.trim() !== "") ? undefined : { field: path, code: "INVALID_TYPE", message: "Takes a list of non-empty entries." };
  }
}
