import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { contextJson, entryJson, pieceJson, serverJson, toolJson } from "../../testing/fixtures";
import { PieceTextDto, SessionContextDto } from "./session-context-dto";

const failsAt = (path: string, dto: string) => (err: unknown) => err instanceof DtoError && err.path === path && err.dto === dto && err.message.includes(path);

describe("SessionContextDto", () => {
  test("reads the contract's JSON and round-trips through toJSON", () => {
    const json = contextJson({ usage: { tools: { Bash: { count: 2, last_at: "2026-11-23T21:00:00Z" } }, skills: {} }, changes: [{ at: "2026-11-23T21:00:00Z", kind: "skill_invoked", subject: "task-tdd", detail: "invoked" }] });
    assert.deepEqual(JSON.parse(JSON.stringify(SessionContextDto.fromJSON(json))), json);
  });

  test("ignores unknown fields at every level", () => {
    const json = contextJson({ entries: [entryJson({ tools: [{ ...toolJson(), future: 1 } as never] })] });
    assert.equal(SessionContextDto.fromJSON({ ...json, future: true }).entries[0]!.tools.length, 1);
  });

  test("names the DTO and the path of a wrong field", () => {
    const wrongFact = contextJson({ entries: [entryJson({ facts: { ...entryJson().facts, headless: "no" as never } })] });
    assert.throws(() => SessionContextDto.fromJSON(wrongFact), failsAt("entries[0].facts.headless", "FactsDto"));
    const noId = { ...contextJson() } as Record<string, unknown>;
    delete noId.session_id;
    assert.throws(() => SessionContextDto.fromJSON(noId), failsAt("session_id", "SessionContextDto"));
  });

  test("rejects an enum value the contract does not list", () => {
    const state = contextJson({ entries: [entryJson({ tools: [toolJson({ state: "maybe" })] })] });
    assert.throws(() => SessionContextDto.fromJSON(state), failsAt("entries[0].tools[0].state", "ToolDto"));
    const status = contextJson({ entries: [entryJson({ mcp_servers: [serverJson({ status: "offline" })] })] });
    assert.throws(() => SessionContextDto.fromJSON(status), failsAt("entries[0].mcp_servers[0].status", "ServerDto"));
  });

  test("rejects a time that is not RFC 3339 and a negative size", () => {
    assert.throws(() => SessionContextDto.fromJSON(contextJson({ entries: [entryJson({ at: "yesterday" })] })), failsAt("entries[0].at", "EntryDto"));
    const piece = contextJson({ entries: [entryJson({ instructions: [pieceJson({ bytes: -1 })] })] });
    assert.throws(() => SessionContextDto.fromJSON(piece), failsAt("entries[0].instructions[0].bytes", "PieceDto"));
  });

  test("reads a piece's changed mark, and its absence", () => {
    const json = contextJson({ entries: [entryJson({ instructions: [pieceJson({ changed: { from_version: "v12", to_version: "v13" } }), pieceJson({ id: "p2" })] })] });
    const pieces = SessionContextDto.fromJSON(json).entries[0]!.instructions;
    assert.deepEqual(pieces[0]!.changed, { from_version: "v12", to_version: "v13" });
    assert.equal(pieces[1]!.changed, null);
  });
});

describe("PieceTextDto", () => {
  test("reads the full text and rejects an answer without it", () => {
    assert.equal(PieceTextDto.fromJSON({ text: "all of it" }).text, "all of it");
    assert.throws(() => PieceTextDto.fromJSON({}), failsAt("text", "PieceTextDto"));
  });
});
