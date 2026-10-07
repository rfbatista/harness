// Both ChannelGateway implementations run the same contract.

import { Codes } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { channelGatewayContract } from "../testing/channel-contract.js";
import { makeMessage, messageDTO } from "../testing/channel-fixtures.js";
import { stubChannelApi } from "../testing/stub-channel-api.js";
import { toChannelEvent, toTaskMessage, toTaskMessages } from "./channel-dto.js";
import { channelGateway } from "./channel-gateway.js";
import { memoryChannel } from "./memory-channel.js";

file("sessions/infrastructure/channel");

channelGatewayContract("memory", (world) => {
  const memory = memoryChannel(world);
  return { gateway: memory.gateway, send: memory.send, deliver: memory.deliver };
});

function http(world) {
  const stub = stubChannelApi(world);
  const gateway = channelGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

channelGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, send: stub.memory.send, deliver: stub.memory.deliver };
});

test("http · a malformed task message is dropped, the stream stays open", async () => {
  const { gateway, stub } = http({});
  const events = [];
  const close = gateway.follow("p1", (e) => events.push(e), () => {});
  await flush();
  stub.pushRaw({ task_message: { id: "", kind: "question" } });
  stub.pushRaw("not even json");
  stub.pushRaw({ review_request: { id: "r1" } });
  stub.pushRaw({ artifact: { id: "a1", kind: "page", revision: 1, scope: "project", attached_ticket_ids: ["t2"], updated_at: "2026-10-07T12:00:00Z" } });
  stub.memory.send({ taskId: "t1", fromSessionId: "d1", toSessionId: "arch", kind: "question", body: "After?" });
  await flush();
  assert.deepEqual(events.map((e) => e.message.body), ["After?"]);
  close();
});

test("a task message reads from the project feed's keyed shape and a session stream's typed one", () => {
  const dto = messageDTO(makeMessage({ id: "m7" }));
  assert.equal(toChannelEvent({ task_message: dto }).message.id, "m7");
  assert.equal(toChannelEvent({ type: "task_message", task_message: dto, seq: 3 }).message.id, "m7");
  assert.equal(toChannelEvent({ session: { id: "s1" } }), null, "other kinds are not the channel's");
  assert.equal(toChannelEvent({ status_check: { delegate_session_id: "d1" } }), null, "the session change that follows carries the check");
  assert.equal(toChannelEvent({ type: "status", status: "running" }), null);
  assert.equal(toChannelEvent(null), null);
});

test("a task message without its optional fields reads them as empty", () => {
  const m = toTaskMessage({ id: "m1", kind: "question", body: "Which port?", created_at: "2026-10-02T14:00:00Z", from_session_id: "d1", to_session_id: "arch", document_ids: null });
  assert.deepEqual([m.subject, m.status, m.verdict, m.inReplyTo, m.documentIds, m.artifactIds, m.delivered, m.deliveredAt], ["", "", "", "", [], [], false, null]);
  assert.ok(Object.isFrozen(m));
});

test("a malformed task message is BAD_RESPONSE", () => {
  const dto = messageDTO(makeMessage());
  assert.throws(() => toTaskMessage({ ...dto, id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toTaskMessage({ ...dto, kind: "gossip" }), Codes.BAD_RESPONSE);
  assert.throws(() => toTaskMessage({ ...dto, status: "napping" }), Codes.BAD_RESPONSE);
  assert.throws(() => toTaskMessage({ ...dto, verdict: "maybe" }), Codes.BAD_RESPONSE);
  assert.throws(() => toTaskMessage({ ...dto, created_at: undefined }), Codes.BAD_RESPONSE);
  assert.throws(() => toTaskMessages({ messages: null }), Codes.BAD_RESPONSE);
});
