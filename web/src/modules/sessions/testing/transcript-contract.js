// The TranscriptGateway contract (../domain/ports.js), run by every
// implementation. A fake that passes it pages like the server does, as far as
// the History tab can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { assert, test } from "../../../shared/testing/test.js";
import { T0 } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").TranscriptGateway} gateway
 * @property {(sessionId: string, ...fields: object[]) => void} append  the CLI writes more of the conversation
 */

/** n entries "1".."n" for a world, oldest first. */
export const numbered = (n, from = 1) => Array.from({ length: n }, (_, i) => ({ text: String(from + i) }));

/** @param {string} name @param {(world: { transcripts?: Record<string, object[]> }) => Subject} makeSubject */
export function transcriptGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const texts = (page) => page.entries.map((e) => e.text);

  contract("no cursor: the newest entries, oldest first, with what lies around them", async () => {
    const { gateway } = makeSubject({ transcripts: { s1: numbered(5) } });
    const page = await gateway.page("s1", { limit: 3 });
    assert.deepEqual(texts(page), ["3", "4", "5"]);
    assert.deepEqual([page.hasOlder, page.hasNewer], [true, false]);
    assert.equal(page.firstCursor, page.entries[0].cursor);
    assert.equal(page.lastCursor, page.entries[2].cursor);
    const all = await gateway.page("s1");
    assert.deepEqual([texts(all).length, all.hasOlder], [5, false]);
  });

  contract("before walks back to the start of the conversation", async () => {
    const { gateway } = makeSubject({ transcripts: { s1: numbered(5) } });
    const newest = await gateway.page("s1", { limit: 2 });
    const older = await gateway.page("s1", { before: newest.firstCursor, limit: 2 });
    assert.deepEqual([texts(older), older.hasOlder, older.hasNewer], [["2", "3"], true, true]);
    const oldest = await gateway.page("s1", { before: older.firstCursor, limit: 2 });
    assert.deepEqual([texts(oldest), oldest.hasOlder], [["1"], false]);
  });

  contract("after catches up with what the CLI wrote since, a page at a time", async () => {
    const { gateway, append } = makeSubject({ transcripts: { s1: numbered(2) } });
    const first = await gateway.page("s1");
    const none = await gateway.page("s1", { after: first.lastCursor });
    assert.deepEqual([none.entries, none.firstCursor, none.lastCursor, none.hasNewer], [[], "", "", false]);
    append("s1", ...numbered(3, 3));
    const next = await gateway.page("s1", { after: first.lastCursor, limit: 2 });
    assert.deepEqual([texts(next), next.hasNewer], [["3", "4"], true]);
    const rest = await gateway.page("s1", { after: next.lastCursor, limit: 2 });
    assert.deepEqual([texts(rest), rest.hasNewer], [["5"], false]);
  });

  contract("an entry keeps its kind, time, tool, error, cut and rendered html", async () => {
    const at = new Date(T0.getTime() + 60_000);
    const { gateway } = makeSubject({
      transcripts: { s1: [{ kind: "tool_result", text: "boom", tool: "Bash", isError: true, truncated: true, at }, { kind: "user", text: "**hi**", html: "<p><strong>hi</strong></p>" }] },
    });
    const [result, user] = (await gateway.page("s1")).entries;
    assert.deepEqual([result.kind, result.tool, result.isError, result.truncated, result.at?.toISOString(), result.html], ["tool_result", "Bash", true, true, at.toISOString(), ""]);
    assert.deepEqual([user.kind, user.tool, user.isError, user.at, user.html], ["user", "", false, null, "<p><strong>hi</strong></p>"]);
  });

  contract("a session without a saved conversation rejects with SESSION_TRANSCRIPT_MISSING until one appears", async () => {
    const { gateway, append } = makeSubject({});
    await assert.rejects(gateway.page("s1"), Codes.SESSION_TRANSCRIPT_MISSING);
    append("s1", { text: "first" });
    assert.deepEqual(texts(await gateway.page("s1")), ["first"]);
  });

  contract("both cursors, or a limit out of 1–500, are INVALID_INPUT", async () => {
    const { gateway } = makeSubject({ transcripts: { s1: numbered(2) } });
    const page = await gateway.page("s1");
    await assert.rejects(gateway.page("s1", { before: page.lastCursor, after: page.firstCursor }), Codes.INVALID_INPUT);
    await assert.rejects(gateway.page("s1", { limit: 0 }), Codes.INVALID_INPUT);
    await assert.rejects(gateway.page("s1", { limit: 501 }), Codes.INVALID_INPUT);
  });
}
