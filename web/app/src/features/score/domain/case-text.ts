// The text form of a case's mock tools: one per line, what a person types and reads back. (The expected calls' form carries JSON: infrastructure/json-calls-text.ts.)

import type { MockTool } from "./bench";

export interface LineError {
  /** 1-based. */
  readonly line: number;
  readonly message: string;
}

const lines = (text: string): { n: number; text: string }[] => text.split("\n").map((t, i) => ({ n: i + 1, text: t.trim() })).filter((l) => l.text !== "");

/** `name = canned result`, or `name ! error result`. */
export function parseMockTools(text: string): { mocks: MockTool[]; errors: LineError[] } {
  const mocks: MockTool[] = [];
  const errors: LineError[] = [];
  for (const l of lines(text)) {
    const match = /^([^\s=!]+)\s*([=!])\s*(.*)$/.exec(l.text);
    if (!match) {
      errors.push({ line: l.n, message: `Line ${l.n}: write a mock tool as name = result, or name ! result for an error result.` });
      continue;
    }
    mocks.push({ name: match[1] ?? "", description: "", result: match[3] ?? "", isError: match[2] === "!" });
  }
  return { mocks, errors };
}

export function formatMocks(mocks: readonly MockTool[]): string {
  return mocks.map((m) => `${m.name} ${m.isError ? "!" : "="} ${m.result}`).join("\n");
}
