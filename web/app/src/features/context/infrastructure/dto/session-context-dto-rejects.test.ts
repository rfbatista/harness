import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { contextJson, entryJson, factsJson, pieceJson, serverJson } from "../../testing/fixtures";
import { SessionContextDto } from "./session-context-dto";

const failsAt = (path: string) => (err: unknown) => err instanceof DtoError && err.path === path;
const without = (o: object, key: string) => Object.fromEntries(Object.entries(o).filter(([k]) => k !== key));
const read = (raw: unknown) => () => SessionContextDto.fromJSON(raw);

describe("SessionContextDto rejects an answer that breaks the contract", () => {
  test("a body that is not an object", () => {
    for (const raw of [null, "ok", [], 3]) assert.throws(read(raw), (e) => e instanceof DtoError);
  });

  test("top-level fields that are missing or of the wrong type", () => {
    assert.throws(read({ ...contextJson(), recorded: "true" }), failsAt("recorded"));
    assert.throws(read({ ...contextJson(), entries: {} }), failsAt("entries"));
    assert.throws(read(without(contextJson(), "changes")), failsAt("changes"));
    assert.throws(read(without(contextJson(), "usage")), failsAt("usage"));
  });

  test("usage whose tools or skills are not maps of count and time", () => {
    assert.throws(read({ ...contextJson(), usage: { tools: [], skills: {} } }), failsAt("usage.tools"));
    assert.throws(read({ ...contextJson(), usage: { tools: { Bash: { count: 1.5, last_at: "2026-11-23T21:00:00Z" } }, skills: {} } }), failsAt("usage.tools.Bash.count"));
    assert.throws(read({ ...contextJson(), usage: { tools: {}, skills: { x: { count: 1, last_at: "later" } } } }), failsAt("usage.skills.x.last_at"));
  });

  test("a change of a kind the contract does not list, or with a bad time", () => {
    assert.throws(read({ ...contextJson(), changes: [{ at: "2026-11-23T21:00:00Z", kind: "tool_refused", subject: "x", detail: "" }] }), failsAt("changes[0].kind"));
    assert.throws(read({ ...contextJson(), changes: [{ at: "noon", kind: "skill_invoked", subject: "x", detail: "" }] }), failsAt("changes[0].at"));
    assert.throws(read({ ...contextJson(), changes: [{ at: "2026-11-23T21:00:00Z", kind: "skill_invoked", subject: "x" }] }), failsAt("changes[0].detail"));
  });

  test("an entry of a kind other than start or resume", () => {
    assert.throws(read(contextJson({ entries: [entryJson({ kind: "restart" })] })), failsAt("entries[0].kind"));
  });

  test("facts with a role or a list the contract does not allow", () => {
    assert.throws(read(contextJson({ entries: [entryJson({ facts: factsJson({ role: "owner" }) })] })), failsAt("entries[0].facts.role"));
    assert.throws(read(contextJson({ entries: [entryJson({ facts: factsJson({ token_savers: ["rtk", 3 as never] }) })] })), failsAt("entries[0].facts.token_savers[1]"));
    assert.throws(read(contextJson({ entries: [entryJson({ facts: factsJson({ approx_tokens: -5 }) })] })), failsAt("entries[0].facts.approx_tokens"));
  });

  test("a piece with an unknown kind, a missing text or a half-formed changed mark", () => {
    assert.throws(read(contextJson({ entries: [entryJson({ instructions: [pieceJson({ kind: "secret" })] })] })), failsAt("entries[0].instructions[0].kind"));
    assert.throws(read(contextJson({ entries: [entryJson({ instructions: [without(pieceJson(), "text") as never] })] })), failsAt("entries[0].instructions[0].text"));
    assert.throws(read(contextJson({ entries: [entryJson({ instructions: [pieceJson({ changed: { from_version: "v1" } as never })] })] })), failsAt("entries[0].instructions[0].changed.to_version"));
  });

  test("a server with an unknown attachment, and a skill or hook missing a field", () => {
    assert.throws(read(contextJson({ entries: [entryJson({ mcp_servers: [serverJson({ attached: "user" })] })] })), failsAt("entries[0].mcp_servers[0].attached"));
    assert.throws(read(contextJson({ entries: [entryJson({ mcp_servers: [{ ...serverJson(), hidden_tools: "none" } as never] })] })), failsAt("entries[0].mcp_servers[0].hidden_tools"));
    assert.throws(read(contextJson({ entries: [entryJson({ skills: [{ name: "x", source: "built_in" } as never] })] })), failsAt("entries[0].skills[0].description"));
    assert.throws(read(contextJson({ entries: [entryJson({ hooks: [{ event: "PreToolUse", matcher: "" } as never] })] })), failsAt("entries[0].hooks[0].rule"));
  });

  test("the second entry's fault is named with its own index", () => {
    const bad = entryJson({ id: "e2", tools: [{ name: "Bash", source: "plugin", server: "", state: "allowed" } as never] });
    assert.throws(read(contextJson({ entries: [entryJson(), bad] })), failsAt("entries[1].tools[0].source"));
  });

  test("an unrecorded session is a valid answer with no entries", () => {
    const dto = SessionContextDto.fromJSON(contextJson({ recorded: false, entries: [] }));
    assert.deepEqual([dto.recorded, dto.entries.length], [false, 0]);
  });
});
