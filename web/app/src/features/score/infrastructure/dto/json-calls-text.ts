// CallsText over JSON: the parameters of a call are a JSON object after the tool's name.

import type { CallsText } from "../../application/ports";
import type { ExpectedCall } from "../../domain/bench";
import type { LineError } from "../../domain/case-text";

/** `tool` accepts any arguments; `tool {"param": value}` names each parameter. */
function parse(text: string): { calls: ExpectedCall[]; errors: LineError[] } {
  const calls: ExpectedCall[] = [];
  const errors: LineError[] = [];
  for (const l of lines(text)) {
    const space = l.text.search(/\s/);
    const tool = space < 0 ? l.text : l.text.slice(0, space);
    const rest = space < 0 ? "" : l.text.slice(space).trim();
    if (rest === "") {
      calls.push({ tool, params: null });
      continue;
    }
    try {
      const params: unknown = JSON.parse(rest);
      if (params === null || typeof params !== "object" || Array.isArray(params)) throw new Error("not an object");
      calls.push({ tool, params: params as Record<string, unknown> });
    } catch {
      errors.push({ line: l.n, message: `Line ${l.n}: the parameters of ${tool} must be a JSON object, like {"path": "README.md"}.` });
    }
  }
  return { calls, errors };
}

function format(calls: readonly ExpectedCall[]): string {
  return calls.map((c) => (c.params === null ? c.tool : `${c.tool} ${JSON.stringify(c.params)}`)).join("\n");
}


const lines = (text: string): { n: number; text: string }[] => text.split("\n").map((t, i) => ({ n: i + 1, text: t.trim() })).filter((l) => l.text !== "");

function formatCall(tool: string, params: Readonly<Record<string, unknown>>): string {
  return Object.keys(params).length === 0 ? tool : `${tool} ${JSON.stringify(params)}`;
}

export const jsonCallsText: CallsText = { parse, format, formatCall };
