// The /api client. The API is RPC-style: reads are GET with query params,
// writes are POST with a JSON body. Every failure becomes a StructuredError.

import { Codes, StructuredError } from "../domain/errors.js";

/**
 * @typedef {object} ApiClient
 * @property {(path: string, query?: Record<string, string|undefined>, signal?: AbortSignal) => Promise<any>} get
 * @property {(path: string, body?: unknown, signal?: AbortSignal) => Promise<any>} post
 */

/**
 * @param {{ base: string, fetch?: typeof globalThis.fetch }} options
 * @returns {ApiClient}
 */
export function apiClient({ base, fetch = globalThis.fetch.bind(globalThis) }) {
  async function send(path, init) {
    let res;
    try {
      res = await fetch(base + path, { ...init, headers: { Accept: "application/json", ...init.headers } });
    } catch (err) {
      if (err?.name === "AbortError") throw err;
      throw new StructuredError(Codes.NETWORK, "The harness server is not reachable.");
    }

    const text = await res.text();
    let body = null;
    if (text) {
      try {
        body = JSON.parse(text);
      } catch {
        throw new StructuredError(Codes.BAD_RESPONSE, `Unreadable response from ${path}.`, res.status);
      }
    }

    if (!res.ok) {
      throw new StructuredError(
        body?.code ?? Codes.UNKNOWN,
        body?.error ?? `${res.status} ${res.statusText}`.trim(),
        res.status,
      );
    }
    return body;
  }

  return {
    get(path, query = {}, signal) {
      const params = new URLSearchParams();
      for (const [key, value] of Object.entries(query)) {
        if (value !== undefined && value !== "") params.set(key, value);
      }
      const qs = params.toString();
      return send(qs ? `${path}?${qs}` : path, { method: "GET", signal });
    },
    post(path, body = {}, signal) {
      return send(path, {
        method: "POST",
        signal,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
    },
  };
}
