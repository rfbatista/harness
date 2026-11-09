import { Codes } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { fakeEventSourceClass, fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { interviewDTO } from "../testing/interview-fixtures.js";
import { interviewsGateway } from "./interviews-gateway.js";
import { toInterview, toInterviewEvent, toInterviewList, toSeedInterviews } from "./interviews-dto.js";

file("sessions/infrastructure/interviews");

test("an interview is read into its model: times as dates, the recommendation as it came", () => {
  const iv = toInterview(interviewDTO({ agent_name: "" }));
  assert.equal(iv.agentName, "claude", "no agent name reads as claude");
  assert.equal(iv.askedAt.toISOString(), "2026-11-08T14:32:00.000Z");
  assert.deepEqual(iv.questions[0].recommended, { option: 1 });
  assert.equal(iv.questions[1].recommended, null);
  assert.equal(iv.questions[0].options[0].description, "");
  assert.equal(iv.answers, null);
});

test("a recommended text is kept as a text", () => {
  const iv = toInterview(interviewDTO({ questions: [{ title: "Name?", body: "", kind: "text", options: [], recommended: { text: "interviews" } }] }));
  assert.deepEqual(iv.questions[0].recommended, { text: "interviews" });
});

test("an interview without an id, a known state or questions is BAD_RESPONSE", () => {
  assert.throws(() => toInterview(interviewDTO({ id: "" })), Codes.BAD_RESPONSE);
  assert.throws(() => toInterview(interviewDTO({ state: "weird" })), Codes.BAD_RESPONSE);
  assert.throws(() => toInterview(interviewDTO({ questions: null })), Codes.BAD_RESPONSE);
  assert.throws(() => toInterviewList({ interviews: [] }), Codes.BAD_RESPONSE);
});

test("the seed leaves out a session whose list it cannot read", () => {
  const seed = toSeedInterviews({ s1: { seq: 2, interviews: [interviewDTO()] }, s2: { seq: "x" } });
  assert.deepEqual(Object.keys(seed), ["s1"]);
  assert.equal(seed.s1.seq, 2);
  assert.deepEqual(toSeedInterviews(undefined), {});
});

test("the gateway reads a session's interviews from the API", async () => {
  const { fetch, calls } = fakeFetch(() => jsonResponse(200, { seq: 3, interviews: [interviewDTO()] }));
  const gateway = interviewsGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api", EventSource: fakeEventSourceClass().EventSource }));
  const list = await gateway.list("s 1");
  assert.equal(list.seq, 3);
  assert.equal(list.interviews[0].id, "iv-1");
  assert.equal(calls[0].url, "/api/sessions/s%201/interviews");
});

test("the gateway follows the session's feed, drops what it cannot read and passes the status on", () => {
  const es = fakeEventSourceClass();
  const gateway = interviewsGateway({}, feed({ base: "/api", EventSource: es.EventSource }));
  const events = [];
  const statuses = [];
  const stop = gateway.follow("s1", (e) => events.push(e), (s) => statuses.push(s));
  const source = es.latest();
  assert.equal(source.url, "/api/sessions/s1/interviews/events");
  source.open();
  source.message({ seq: 1, kind: "interview_asked", interview: interviewDTO() });
  source.message({ seq: 2, kind: "something_else" });
  source.message({ seq: 3, kind: "interview_changed", interview: interviewDTO({ state: "sent" }) });
  assert.deepEqual(events.map((e) => `${e.seq}:${e.kind}:${e.interview.state}`), ["1:interview_asked:open", "3:interview_changed:sent"]);
  assert.ok(statuses.includes("live"));
  stop();
  assert.equal(source.closed, true);
  assert.throws(() => toInterviewEvent({ seq: 1, kind: "nope" }), Codes.BAD_RESPONSE);
});

const answersGateway = (handler) => {
  const { fetch, calls } = fakeFetch(handler);
  return { gateway: interviewsGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api", EventSource: fakeEventSourceClass().EventSource })), calls };
};

test("submit posts the answers as the contract has them and reads the interview back", async () => {
  const { gateway, calls } = answersGateway(() => jsonResponse(200, { interview: interviewDTO({ state: "sent", ended_at: "2026-11-08T14:41:00Z", delivery: "queued" }) }));
  const answers = [{ chosen: [1], text: "", note: "because", skipped: false }, { chosen: [], text: "", note: "", skipped: true }];
  const iv = await gateway.submit("iv 1", answers);
  assert.equal(calls[0].url, "/api/interviews/iv%201/answers");
  assert.equal(calls[0].init.method, "POST");
  assert.deepEqual(JSON.parse(calls[0].init.body), { answers });
  assert.deepEqual([iv.state, iv.delivery], ["sent", "queued"]);
});

test("dismiss posts the note, or an empty one, and reads the interview back", async () => {
  const { gateway, calls } = answersGateway(() => jsonResponse(200, { interview: interviewDTO({ state: "dismissed", ended_at: "2026-11-08T14:41:00Z", delivery: "delivered" }) }));
  const iv = await gateway.dismiss("iv-1", "not now");
  assert.equal(calls[0].url, "/api/interviews/iv-1/dismiss");
  assert.deepEqual(JSON.parse(calls[0].init.body), { note: "not now" });
  assert.equal(iv.state, "dismissed");
  await gateway.dismiss("iv-1", "");
  assert.deepEqual(JSON.parse(calls[1].init.body), { note: "" });
});

test("a refusal that carries the interview comes back with it read into the model; any other failure is left as it is", async () => {
  const refused = interviewDTO({ state: "expired", ended_at: "2026-11-08T14:41:00Z" });
  const { gateway } = answersGateway((url) =>
    url.endsWith("/answers") ? jsonResponse(409, { error: "the session ended", code: "INTERVIEW_EXPIRED", details: { interview: refused } }) : jsonResponse(500, { error: "boom" }),
  );
  const err = await gateway.submit("iv-1", []).then(() => null, (e) => e);
  assert.equal(err.code, "INTERVIEW_EXPIRED");
  assert.equal(err.details.interview.state, "expired");
  assert.ok(err.details.interview.askedAt instanceof Date);
  const down = await gateway.dismiss("iv-1", "").then(() => null, (e) => e);
  assert.equal(down.status, 500);
});
