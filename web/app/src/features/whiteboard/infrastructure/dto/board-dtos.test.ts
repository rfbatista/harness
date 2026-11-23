import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { DtoError } from "../../../../shared/infrastructure/dto/dto-error";
import { TaskBranchesDto } from "./branches-dto";
import { BoardEventDto } from "./event-dto";
import { ReviewRequestDto, TaskMessageDto, TaskMessageListDto } from "./message-dto";
import { OpenSessionDto, SessionSelectedDto, ViewChosenDto, ViewShownDto } from "./page-event-dto";
import { BoardSessionDto, BoardSessionListDto } from "./session-dto";

const at = "2026-11-23T12:00:00Z";
const session = { id: "s1", status: "running", role: "delegate", created_at: at, updated_at: at };
const message = { id: "m1", from_session_id: "a", to_session_id: "b", kind: "question", created_at: at };
const rejects = (fn: () => unknown) => assert.throws(fn, DtoError);

describe("session DTO rejection", () => {
  test("reads a minimal session and defaults the optional fields", () => {
    const s = BoardSessionDto.fromJSON(session);
    assert.equal(s.pending_approvals, 0);
    assert.equal(s.interrupted_at, null);
    assert.equal(s.brought_under, false);
  });

  test("rejects what is not a session", () => {
    for (const bad of [null, "s1", 3, [], {}, { ...session, id: 7 }, { ...session, id: undefined }, { ...session, role: "boss" }, { ...session, status: "" }, { ...session, created_at: "yesterday" }, { ...session, updated_at: undefined }, { ...session, pending_approvals: -1 }, { ...session, pending_approvals: 1.5 }, { ...session, pending_approvals: "2" }, { ...session, branch: 4 }, { ...session, interrupted_at: "not a time" }]) {
      rejects(() => BoardSessionDto.fromJSON(bad));
    }
  });

  test("a bad item names its place in the list", () => {
    assert.throws(
      () => BoardSessionListDto.fromJSON({ sessions: [session, { ...session, status: "bogus" }] }),
      (err: unknown) => err instanceof DtoError && String(err.message).includes("sessions[1]"),
    );
    rejects(() => BoardSessionListDto.fromJSON({ sessions: {} }));
    rejects(() => BoardSessionListDto.fromJSON([]));
  });
});

describe("message DTO rejection", () => {
  test("reads a message and a review request", () => {
    assert.equal(TaskMessageDto.fromJSON(message).kind, "question");
    assert.equal(ReviewRequestDto.fromJSON({ id: "r1", state: "pending", created_at: at }).state, "pending");
  });

  test("rejects a message with no id, an unknown kind or status, or a bad time", () => {
    for (const bad of [null, [], {}, { ...message, id: undefined }, { ...message, kind: "shout" }, { ...message, status: "sleeping" }, { ...message, created_at: 5 }, { ...message, from_session_id: 3 }]) {
      rejects(() => TaskMessageDto.fromJSON(bad));
    }
    rejects(() => TaskMessageListDto.fromJSON({ messages: "x" }));
  });

  test("rejects a review request with an unknown state or no time", () => {
    for (const bad of [null, {}, { id: "r1", state: "maybe", created_at: at }, { id: "r1", state: "pending" }, { state: "pending", created_at: at }]) {
      rejects(() => ReviewRequestDto.fromJSON(bad));
    }
  });
});

describe("branches DTO rejection", () => {
  test("rejects a list that is not one, a branch with no name, or pushed that is not an array", () => {
    rejects(() => TaskBranchesDto.fromJSON({ sessions: "x" }));
    rejects(() => TaskBranchesDto.fromJSON({ sessions: [{ branches: [] }] }));
    rejects(() => TaskBranchesDto.fromJSON({ sessions: [{ session_id: "s1", branches: [{ is_session_branch: true }] }] }));
    rejects(() => TaskBranchesDto.fromJSON({ sessions: [{ session_id: "s1", branches: [{ name: "x", pushed: 3 }] }] }));
  });

  test("a null branches list reads as empty", () => {
    assert.deepEqual(TaskBranchesDto.fromJSON({ sessions: [{ session_id: "s1", branches: null }] }).sessions[0]?.branches, []);
  });
});

describe("feed event DTO", () => {
  test("a bad session inside an event is rejected, but an unknown event reads as nothing", () => {
    rejects(() => BoardEventDto.fromJSON({ session: { ...session, status: "bogus" } }));
    rejects(() => BoardEventDto.fromJSON({ task_message: { ...message, kind: "shout" } }));
    rejects(() => BoardEventDto.fromJSON(null));
    const none = BoardEventDto.fromJSON({ something_new: {} });
    assert.equal(none.session, null);
    assert.equal(none.task_message, null);
    assert.equal(none.review_request, null);
    assert.equal(none.session_branches, null);
  });

  test("a deleted session needs no valid body, only its id when it has one", () => {
    assert.equal(BoardEventDto.fromJSON({ session: { id: "s9" }, deleted: true }).deleted_id, "s9");
    assert.equal(BoardEventDto.fromJSON({ session: {}, deleted: true }).deleted_id, null);
  });
});

describe("page event DTO rejection", () => {
  test("a view must be list or whiteboard", () => {
    rejects(() => ViewChosenDto.fromJSON({ view: "timeline" }));
    rejects(() => ViewChosenDto.fromJSON({}));
    rejects(() => ViewShownDto.fromJSON({ view: 1 }));
    assert.equal(ViewShownDto.fromJSON({ view: "whiteboard" }).session_id, "");
  });

  test("opening a session needs its id; a selection may be empty", () => {
    rejects(() => OpenSessionDto.fromJSON({ tab: "context" }));
    rejects(() => OpenSessionDto.fromJSON(null));
    assert.equal(OpenSessionDto.fromJSON({ session_id: "s1" }).tab, "");
    assert.equal(SessionSelectedDto.fromJSON({}).session_id, "");
    rejects(() => SessionSelectedDto.fromJSON("s1"));
  });
});
