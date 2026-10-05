// Test doubles for the browser APIs infrastructure depends on.

/** A fetch that answers from `handler(url, init)`; records every call. */
export function fakeFetch(handler) {
  const calls = [];
  const fetch = async (url, init = {}) => {
    calls.push({ url, init });
    return handler(url, init);
  };
  return { fetch, calls };
}

/** A Response with a JSON body (or none when body is undefined). */
export function jsonResponse(status, body) {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** An EventSource the test drives: open(), message(data), fail(). */
export function fakeEventSourceClass() {
  const instances = [];
  class FakeEventSource {
    constructor(url) {
      this.url = url;
      this.closed = false;
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      instances.push(this);
    }
    open() {
      this.onopen?.({});
    }
    message(data) {
      this.onmessage?.({ data: typeof data === "string" ? data : JSON.stringify(data) });
    }
    fail() {
      this.onerror?.({});
    }
    close() {
      this.closed = true;
    }
  }
  return { EventSource: FakeEventSource, instances, latest: () => instances.at(-1) };
}

/** setTimeout / clearTimeout that only fire when the test calls tick(). */
export function manualTimers() {
  let nextId = 1;
  const pending = new Map();
  return {
    setTimeout(fn, ms) {
      const id = nextId++;
      pending.set(id, { fn, ms });
      return id;
    },
    clearTimeout(id) {
      pending.delete(id);
    },
    /** Delays of the timers waiting to fire, in order. */
    delays: () => [...pending.values()].map((t) => t.ms),
    /** Fires every pending timer. */
    tick() {
      const due = [...pending.values()];
      pending.clear();
      for (const t of due) t.fn();
    },
  };
}

/** Lets queued microtasks (promise callbacks) run. */
export const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
