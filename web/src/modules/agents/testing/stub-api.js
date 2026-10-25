// A stand-in for the harness server's catalog routes: a fetch that speaks the
// server's wire format over the memory server. The real catalogGateway runs
// the contract suite against it, and the test reads what was sent.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryCatalogServer } from "../infrastructure/memory-gateway.js";

export function stubCatalogApi(world) {
  const server = memoryCatalogServer(world);
  /** Every request: [method, path, body]. */
  const requests = [];

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : Object.fromEntries(url.searchParams);
    requests.push([method, url.pathname, body]);
    const path = url.pathname.replace(/^\/api/, "");
    try {
      return jsonResponse(200, method === "GET" ? await server.api.get(path, body) : await server.api.post(path, body));
    } catch (err) {
      return jsonResponse(err.status ?? 500, { error: err.message, code: codeOf(err), ...err.details });
    }
  }
  return { fetch, requests, server };
}
