// A typed client over fetch for the harness API. A non-2xx answer carries the
// API's error body, { error: message, code }, and becomes an ApiError.

import { ApiErrorDto } from "./dto/api-error-dto";

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly fixUrl: string | undefined;

  constructor(code: string, message: string, status: number, fixUrl?: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.fixUrl = fixUrl;
  }
}

export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

/** What may be sent: a DTO, which the client stringifies through its toJSON(). */
export interface Sendable {
  toJSON(): unknown;
}

/** Answers are raw: a caller reads them through a DTO's fromJSON, and sends a DTO. */
export interface ApiClient {
  get(path: string, query?: Record<string, string>, signal?: AbortSignal): Promise<unknown>;
  post(path: string, body?: Sendable, signal?: AbortSignal): Promise<unknown>;
}

const NETWORK_ERROR = "NETWORK_ERROR";

export function apiClient(fetcher: FetchLike = (input, init) => fetch(input, init), base = "/api"): ApiClient {
  async function send(path: string, init: RequestInit): Promise<unknown> {
    let response: Response;
    // A write is about to change what a page shows: what was prefetched is read before it (web/src/prefetch.js).
    if (init.method !== "GET") globalThis.dispatchEvent?.(new Event("api-write"));
    try {
      response = await fetcher(base + path, init);
    } catch (cause) {
      if (cause instanceof Error && cause.name === "AbortError") throw cause;
      throw new ApiError(NETWORK_ERROR, cause instanceof Error ? cause.message : "network error", 0);
    }
    if (!response.ok) throw await failure(response);
    if (response.status === 204) return undefined;
    try {
      return (await response.json()) as unknown;
    } catch {
      // A login page or a proxy's page answered with 200.
      throw new ApiError("BAD_RESPONSE", "The harness server answered something that is not JSON.", response.status);
    }
  }
  return {
    get: (path, query, signal) => send(path + queryString(query), { method: "GET", headers: { Accept: "application/json" }, signal }),
    post: (path, body, signal) =>
      send(path, {
        signal,
        method: "POST",
        headers: { Accept: "application/json", "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      }),
  };
}

function queryString(query?: Record<string, string>): string {
  if (!query) return "";
  const text = new URLSearchParams(query).toString();
  return text === "" ? "" : `?${text}`;
}

async function failure(response: Response): Promise<ApiError> {
  try {
    const body = ApiErrorDto.fromJSON(await response.json());
    return new ApiError(body.code, body.error, response.status, body.fixUrl);
  } catch {
    return new ApiError(`HTTP_${response.status}`, response.statusText, response.status);
  }
}
