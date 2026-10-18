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

/** An XMLHttpRequest the test drives: progress(loaded, total), respond(status, body), fail(). */
export function fakeXHRClass() {
  const instances = [];
  class FakeXMLHttpRequest {
    constructor() {
      this.upload = { onprogress: null };
      this.onload = null;
      this.onerror = null;
      this.onabort = null;
      this.headers = {};
      this.status = 0;
      this.statusText = "";
      this.responseText = "";
      this.aborted = false;
      instances.push(this);
    }
    open(method, url) {
      this.method = method;
      this.url = url;
    }
    setRequestHeader(name, value) {
      this.headers[name] = value;
    }
    send(body) {
      this.body = body;
    }
    abort() {
      this.aborted = true;
      this.onabort?.({});
    }
    progress(loaded, total) {
      this.upload.onprogress?.({ lengthComputable: true, loaded, total });
    }
    respond(status, body) {
      this.status = status;
      this.responseText = typeof body === "string" ? body : JSON.stringify(body);
      this.onload?.({});
    }
    fail() {
      this.onerror?.({});
    }
  }
  return { XMLHttpRequest: FakeXMLHttpRequest, instances, latest: () => instances.at(-1) };
}

/**
 * Browser tabs of one profile, for code that shares work across tabs: each
 * tab() has its own Web Locks manager and BroadcastChannel, seeing the
 * others' as the browser would. A lock is granted in request order, released
 * when its callback's promise settles, taken over by `steal`, and dropped
 * with its tab when the tab closes (or crashes). A message posted on a
 * channel reaches every other open channel of that name, synchronously.
 */
export function fakeCrossTab() {
  const locks = new Map(); // name -> { held: entry|null, queue: entry[] }
  const channels = new Set();

  function grant(name) {
    const l = locks.get(name);
    if (!l || l.held || l.queue.length === 0) return;
    const entry = l.queue.shift();
    l.held = entry;
    let done;
    try {
      done = Promise.resolve(entry.callback({ name }));
    } catch (err) {
      done = Promise.reject(err);
    }
    done.then(
      (value) => release(entry, () => entry.resolve(value)),
      (err) => release(entry, () => entry.reject(err)),
    );
  }
  function release(entry, settle) {
    const l = locks.get(entry.name);
    if (l.held !== entry) return; // stolen or dropped already
    l.held = null;
    settle();
    grant(entry.name);
  }
  function abort(entry) {
    const err = new Error("the lock was released");
    err.name = "AbortError";
    entry.reject(err);
  }

  function tab() {
    const mine = new Set();
    let open = true;

    const tabLocks = {
      request(name, options, callback) {
        if (typeof options === "function") [options, callback] = [{}, options];
        let l = locks.get(name);
        if (!l) locks.set(name, (l = { held: null, queue: [] }));
        return new Promise((resolve, reject) => {
          if (!open) return; // a closed tab runs nothing
          const entry = { name, callback, resolve, reject, tab: mine };
          mine.add(entry);
          if (options.steal) {
            const held = l.held;
            l.held = null;
            if (held) abort(held);
            l.queue.unshift(entry);
          } else {
            l.queue.push(entry);
          }
          grant(name);
        });
      },
    };

    class FakeBroadcastChannel {
      constructor(name) {
        this.name = name;
        this.onmessage = null;
        this.closed = false;
        this.tabOpen = () => open;
        channels.add(this);
      }
      postMessage(data) {
        if (this.closed || !open) return;
        const copy = structuredClone(data);
        for (const c of [...channels]) {
          if (c === this || c.closed || !c.tabOpen() || c.name !== this.name) continue;
          c.onmessage?.({ data: copy });
        }
      }
      close() {
        this.closed = true;
        channels.delete(this);
      }
    }

    const listeners = new Map();
    const window = {
      addEventListener(type, fn) {
        if (!listeners.has(type)) listeners.set(type, []);
        listeners.get(type).push(fn);
      },
    };

    return {
      locks: tabLocks,
      BroadcastChannel: FakeBroadcastChannel,
      window,
      /** Closes the tab: `pagehide` runs first unless it crashed. Its locks go with it. */
      close({ crash = false } = {}) {
        if (!crash) for (const fn of listeners.get("pagehide") ?? []) fn({});
        open = false;
        for (const entry of mine) {
          const l = locks.get(entry.name);
          const i = l.queue.indexOf(entry);
          if (i >= 0) l.queue.splice(i, 1);
          if (l.held === entry) {
            l.held = null;
            grant(entry.name);
          }
        }
        mine.clear();
      },
    };
  }

  return { tab };
}
