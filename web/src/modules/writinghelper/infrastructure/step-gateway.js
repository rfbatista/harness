// StepGateway over POST /api/run_writing_helper_step. The answer streams
// SSE-framed events (delta…, then result or error); it is read with a stream
// reader, not the shared page stream. Aborting the signal is Stop.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { sseParser } from "./sse.js";

/**
 * @param {{ base: string, fetch?: typeof globalThis.fetch }} options
 * @returns {import("../domain/ports.js").StepGateway}
 */
export function stepGateway({ base, fetch = globalThis.fetch.bind(globalThis) }) {
  return {
    async runStep(request, { signal, onDelta } = {}) {
      let res;
      try {
        res = await fetch(`${base}/run_writing_helper_step`, {
          method: "POST",
          signal,
          headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
          body: JSON.stringify(request),
        });
      } catch (err) {
        if (err?.name === "AbortError") throw err;
        throw new StructuredError(Codes.NETWORK, "The harness server is not reachable.");
      }
      if (!res.ok) throw await refusal(res);
      if (!res.body) throw failed("The helper sent no answer.");
      return readStream(res.body, onDelta);
    },
  };
}

const failed = (message) => new StructuredError(Codes.WRITING_HELPER_FAILED, message);

/** The JSON error a refused step answers with, as a StructuredError. */
async function refusal(res) {
  let body = null;
  try {
    body = JSON.parse(await res.text());
  } catch {
    // not JSON: the status line will do
  }
  return new StructuredError(
    body?.code ?? Codes.UNKNOWN,
    body?.message ?? body?.error ?? `${res.status} ${res.statusText}`.trim(),
    res.status,
    body?.fix_url ? { fix_url: body.fix_url } : body?.details,
  );
}

/** @param {ReadableStream<Uint8Array>} body @param {((text: string) => void) | undefined} onDelta */
async function readStream(body, onDelta) {
  /** @type {object | undefined} */
  let result;
  /** @type {StructuredError | undefined} */
  let error;
  const parser = sseParser((name, data) => {
    let payload;
    try {
      payload = JSON.parse(data);
    } catch {
      error ??= failed("The helper sent an answer that cannot be read.");
      return;
    }
    if (name === "delta") onDelta?.(String(payload.text ?? ""));
    else if (name === "result") result = payload;
    else if (name === "error") {
      error = new StructuredError(payload.code ?? Codes.WRITING_HELPER_FAILED, payload.message ?? "The helper failed.", undefined, payload.fix_url ? { fix_url: payload.fix_url } : undefined);
    }
  });

  const reader = body.getReader();
  const decoder = new TextDecoder();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      parser.push(decoder.decode(value, { stream: true }));
    }
    parser.end();
  } catch (err) {
    if (err?.name === "AbortError") throw err;
    throw failed("The connection to the helper was lost.");
  }
  if (error) throw error;
  if (!result) throw failed("The helper stopped before it finished.");
  return result;
}
