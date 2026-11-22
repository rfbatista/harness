// A typed client over fetch for the harness API. A non-2xx answer carries the
// API's error body, { error: message, code }, and becomes an ApiError.

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

export interface ApiClient {
  get<T>(path: string, query?: Record<string, string>, signal?: AbortSignal): Promise<T>;
  post<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T>;
}

const NETWORK_ERROR = "NETWORK_ERROR";

export function apiClient(fetcher: FetchLike = (input, init) => fetch(input, init), base = "/api"): ApiClient {
  async function send<T>(path: string, init: RequestInit): Promise<T> {
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
    if (response.status === 204) return undefined as T;
    try {
      return (await response.json()) as T;
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
    const body = (await response.json()) as { error?: string; code?: string };
    return new ApiError(body.code ?? `HTTP_${response.status}`, body.error ?? response.statusText, response.status);
  } catch {
    return new ApiError(`HTTP_${response.status}`, response.statusText, response.status);
  }
}
