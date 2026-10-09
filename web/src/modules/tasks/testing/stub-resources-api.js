// A stand-in for the server's resource and comment routes, backed by the
// memory gateway and speaking the real wire format: the JSON routes (fetch),
// the multipart upload with its part order and progress (XMLHttpRequest),
// and the `ticket_resource` / `ticket_comment` changes on the project feed
// among the session and ticket changes the gateway must ignore
// (EventSource). The real resourcesGateway runs the contract suite against it.

import { Codes, codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryResources } from "../infrastructure/memory-resources.js";
import { commentDTO, resourceDTO } from "./resource-fixtures.js";

const STATUS = { TICKET_NOT_FOUND: 404, RESOURCE_NOT_FOUND: 404, COMMENT_NOT_FOUND: 404, RESOURCE_TOO_LARGE: 413, INVALID_INPUT: 400 };
const statusOf = (err) => STATUS[codeOf(err)] ?? 500;

export function stubResourcesApi(world) {
  const memory = memoryResources(world);
  const { gateway } = memory;
  const sources = new Set();

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const body = init.body ? JSON.parse(init.body) : {};
    const ticket = url.searchParams.get("ticket_id") ?? "";
    try {
      switch (`${init.method ?? "GET"} ${url.pathname}`) {
        case "GET /api/list_ticket_resources":
          return jsonResponse(200, { resources: (await gateway.listResources(ticket)).map(resourceDTO) });
        case "POST /api/add_ticket_link":
          return jsonResponse(200, { resource: resourceDTO(await gateway.addLink({ ticketId: body.ticket_id, url: body.url, title: body.title })) });
        case "POST /api/update_ticket_resource":
          return jsonResponse(200, { resource: resourceDTO(await gateway.retitle(body.resource_id, body.title)) });
        case "POST /api/delete_ticket_resource":
          await gateway.removeResource(body.resource_id);
          return jsonResponse(200, {});
        case "GET /api/list_ticket_comments":
          return jsonResponse(200, { comments: (await gateway.listComments(ticket)).map(commentDTO) });
        case "POST /api/add_ticket_comment":
          return jsonResponse(200, { comment: commentDTO(await gateway.addComment(body.ticket_id, body.body)) });
        case "POST /api/update_ticket_comment":
          return jsonResponse(200, { comment: commentDTO(await gateway.editComment(body.comment_id, body.body)) });
        case "POST /api/delete_ticket_comment":
          await gateway.removeComment(body.comment_id);
          return jsonResponse(200, {});
        default:
          return jsonResponse(404, { error: `no route ${url.pathname}` });
      }
    } catch (err) {
      return jsonResponse(statusOf(err), { error: err.message, code: codeOf(err) });
    }
  }

  /** POST /api/upload_ticket_resource, multipart: ticket_id, title?, then file. */
  class StubXMLHttpRequest {
    constructor() {
      this.upload = { onprogress: null };
      this.onload = null;
      this.onerror = null;
      this.onabort = null;
      this.status = 0;
      this.statusText = "";
      this.responseText = "";
      this.abortController = new AbortController();
    }
    open(method, url) {
      this.method = method;
      this.url = new URL(url, "http://harness.test");
    }
    setRequestHeader() {}
    abort() {
      this.abortController.abort();
    }
    answer(status, body) {
      this.status = status;
      this.responseText = JSON.stringify(body);
      this.onload?.({});
    }
    async send(form) {
      if (this.method !== "POST" || this.url.pathname !== "/api/upload_ticket_resource") return this.answer(404, { error: "no route" });
      const names = [...form.keys()];
      const fileAt = names.indexOf("file");
      const late = (name) => names.includes(name) && names.indexOf(name) > fileAt;
      if (fileAt < 0 || !names.includes("ticket_id") || late("ticket_id") || late("title")) {
        return this.answer(400, { error: "ticket_id and title must come before file", code: Codes.INVALID_INPUT });
      }
      try {
        const resource = await gateway.upload({
          ticketId: form.get("ticket_id"),
          title: form.get("title") ?? "",
          file: form.get("file"),
          signal: this.abortController.signal,
          onProgress: (loaded, total) => this.upload.onprogress?.({ lengthComputable: true, loaded, total }),
        });
        this.answer(200, { resource: resourceDTO(resource) });
      } catch (err) {
        if (err?.name === "AbortError") this.onabort?.({});
        else this.answer(statusOf(err), { error: err.message, code: codeOf(err) });
      }
    }
  }

  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.projectId = url.pathname === "/api/events" ? (url.searchParams.get("project_id") ?? "") : "";
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      this.unfollow = gateway.follow(this.projectId, (change) => this.send(toWire(change)), () => {});
      queueMicrotask(() => {
        this.onopen?.({});
        // The project feed carries more than resources; the gateway must ignore the rest.
        this.send({ ticket: { id: "t1", project_id: this.projectId, title: "x", status: "todo" } });
        this.send({ artifact: { id: "a1", kind: "page" } });
      });
    }
    send(dto) {
      this.onmessage?.({ data: typeof dto === "string" ? dto : JSON.stringify(dto) });
    }
    close() {
      this.unfollow();
      sources.delete(this);
    }
  }

  function toWire(change) {
    switch (change.kind) {
      case "resource":
        return { ticket_resource: resourceDTO(change.resource) };
      case "resource-deleted":
        return { ticket_resource: { id: change.id, ticket_id: change.ticketId, project_id: "p1" }, deleted: true };
      case "comment":
        return { ticket_comment: commentDTO(change.comment) };
      case "comment-deleted":
        return { ticket_comment: { id: change.id, ticket_id: change.ticketId, project_id: "p1" }, deleted: true };
      case "session":
        return { session: { id: change.id, ticket_id: change.ticketId, project_id: "p1", status: "running" } };
      case "session-deleted":
        return { session: { id: change.id }, deleted: true };
      default:
        return { ticket: { id: change.id }, deleted: true };
    }
  }

  return {
    fetch,
    XMLHttpRequest: StubXMLHttpRequest,
    EventSource: StubEventSource,
    memory,
    /** Pushes any line to every project feed open on the project, as a buggy server might. */
    pushProject(projectId, dto) {
      for (const s of sources) if (s.projectId === projectId) s.send(dto);
    },
  };
}
