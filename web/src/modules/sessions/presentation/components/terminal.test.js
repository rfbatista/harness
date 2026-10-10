import { mount } from "../../../../shared/testing/alpine.js";
import { flush, manualTimers } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTerminals } from "../../infrastructure/memory-terminals.js";
import { terminal } from "./terminal.js";

file("sessions/presentation/terminal");

function fakeScreen() {
  const screen = { drawn: [], written: "", size: { cols: 80, rows: 24 }, nextFit: null, focused: 0, disposed: false, input: null, fontSizes: [], bottomed: 0, cursorHidden: 0, options: null };
  const decoder = new TextDecoder();
  const create = (container, input) => {
    screen.input = input;
    screen.options = input;
    return {
      setFontSize: (px) => screen.fontSizes.push(px),
      scrollToBottom: () => screen.bottomed++,
      hideCursor: () => screen.cursorHidden++,
      draw: (s) => {
        screen.drawn.push(s);
        screen.size = { cols: s.cols, rows: s.rows };
      },
      write: (bytes) => (screen.written += decoder.decode(bytes)),
      fit: () => {
        const next = screen.nextFit;
        screen.nextFit = null;
        if (next) screen.size = next;
        return next;
      },
      focus: () => screen.focused++,
      dispose: () => (screen.disposed = true),
    };
  };
  return { screen, create };
}

function setup(history = [], { kind = "agent", textSize = () => 13, now = () => 1000 } = {}) {
  const terms = memoryTerminals({ greeting: "claude is ready", history });
  const { screen, create } = fakeScreen();
  const timers = manualTimers();
  let resized = null;
  const el = document.createElement("div");
  const refs = { screen: document.createElement("div") };
  const mounted = mount(
    () => terminal({ terminals: terms.gateway, createScreen: create, timers, kind, textSize, now, observeResize: (_, cb) => ((resized = cb), () => {}) })("s1"),
    { el },
  );
  Object.defineProperty(mounted.instance, "$refs", { value: refs });
  mounted.instance.init();
  return { ...mounted, terms, screen, timers, resize: () => resized() };
}

const press = (k) => ({ key: k, shiftKey: false, altKey: false, ctrlKey: false, metaKey: false });

test("draws the snapshot, then takes the pane's size, and leaves the focus where it is", async () => {
  const { instance, screen, terms } = setup();
  assert.equal(instance.state, "connecting");
  screen.nextFit = { cols: 132, rows: 40 };
  await flush();
  assert.equal(instance.state, "live");
  assert.equal(screen.drawn[0].screen, "claude is ready");
  assert.deepEqual(terms.sent.get("s1").at(-1), { type: "resize", size: { cols: 132, rows: 40 } });
  assert.equal(screen.focused, 0, "a snapshot never moves the focus");
});

test("the snapshot's history reaches the screen with it, so the screen can draw it above the live rows", async () => {
  const { screen } = setup(["$ go test ./...", "ok"]);
  await flush();
  assert.equal(screen.drawn[0].scrollback, "$ go test ./...\nok");
});

test("typing goes to the PTY and its output comes back to the screen", async () => {
  const { screen, terms } = setup();
  await flush();
  assert.equal(screen.input.onKey(press("l")), true);
  assert.equal(screen.input.onKey(press("s")), true);
  screen.input.onPaste(" -la");
  assert.equal(screen.written, "ls -la");
  assert.deepEqual(
    terms.sent.get("s1").filter((m) => m.type !== "resize").map((m) => m.type),
    ["key", "key", "paste"],
  );
});

test("resizing the pane resizes the PTY once it settles", async () => {
  const { screen, terms, timers, resize } = setup();
  await flush();
  screen.nextFit = { cols: 90, rows: 20 };
  resize();
  assert.equal(terms.sent.get("s1")?.filter((m) => m.type === "resize").length ?? 0, 0, "not while the pane is still changing");
  timers.tick();
  assert.deepEqual(terms.sent.get("s1").at(-1), { type: "resize", size: { cols: 90, rows: 20 } });
});

test("a burst of pane changes (a window drag, a header growing) sends one resize", async () => {
  const { screen, terms, timers, resize } = setup();
  await flush();
  resize();
  resize();
  screen.nextFit = { cols: 100, rows: 30 };
  resize();
  assert.deepEqual(timers.delays().length, 1, "each change restarts the one wait");
  timers.tick();
  assert.deepEqual(
    terms.sent.get("s1").filter((m) => m.type === "resize"),
    [{ type: "resize", size: { cols: 100, rows: 30 } }],
  );
});

test("a snapshot already at the pane's size sends no resize; one at another size sends the pane's", async () => {
  const { instance, screen, terms } = setup();
  await flush();
  assert.equal(terms.sent.get("s1")?.filter((m) => m.type === "resize").length ?? 0, 0, "the server has this size already");
  screen.nextFit = { cols: 120, rows: 35 };
  instance.connect();
  await flush();
  assert.deepEqual(terms.sent.get("s1").filter((m) => m.type === "resize"), [{ type: "resize", size: { cols: 120, rows: 35 } }]);
});

test("a pane change that leaves the size as it was sends nothing (a hidden pane fits to nothing)", async () => {
  const { terms, timers, resize } = setup();
  await flush();
  resize();
  timers.tick();
  assert.equal(terms.sent.get("s1")?.filter((m) => m.type === "resize").length ?? 0, 0);
});

test("destroying the terminal drops a pending fit", async () => {
  const { instance, screen, terms, timers, resize } = setup();
  await flush();
  screen.nextFit = { cols: 90, rows: 20 };
  resize();
  instance.destroy();
  assert.equal(timers.delays().length, 0);
  assert.equal(terms.sent.get("s1")?.filter((m) => m.type === "resize").length ?? 0, 0);
});

test("output, titles, resizes and a reconnect's snapshot never move the focus; asking for it does", async () => {
  const { instance, screen, terms, timers, resize } = setup();
  await flush();
  terms.print("s1", "more");
  terms.retitle("s1", "claude: tests");
  screen.nextFit = { cols: 90, rows: 20 };
  resize();
  timers.tick();
  instance.connect();
  await flush();
  assert.equal(screen.drawn.length, 2, "the reconnect drew a fresh snapshot");
  assert.equal(screen.focused, 0);
  instance.focus();
  assert.equal(screen.focused, 1);
});

test("Reconnect focuses the terminal on the press, not on the snapshot that follows", async () => {
  const { instance, screen } = setup();
  await flush();
  instance.state = "offline";
  instance.reconnect();
  assert.equal(screen.focused, 1, "focused on the press");
  await flush();
  assert.equal(screen.drawn.length, 2);
  assert.equal(screen.focused, 1, "the snapshot after it adds nothing");
});

test("destroy detaches without stopping the session", async () => {
  const { instance, screen, terms } = setup();
  await flush();
  instance.destroy();
  assert.ok(screen.disposed);
  assert.equal(terms.sent.get("s1").at(-1).type, "close");
});

const statuses = (dispatched) => dispatched.filter((d) => d.name === "terminal-status").map((d) => d.detail);
const resizesOf = (terms) => terms.sent.get("s1")?.filter((m) => m.type === "resize") ?? [];

test("reports its state up as it goes: connecting, then live, with its kind and window title", async () => {
  const { instance, dispatched, terms } = setup([], { kind: "run" });
  assert.equal(instance.state, "connecting");
  assert.deepEqual(statuses(dispatched)[0], { id: "s1", kind: "run", state: "connecting", title: "", exitCode: null, lostAt: null, scrolledUp: false, newLines: 0 });
  await flush();
  terms.retitle("s1", "✳ Ionizing…");
  const last = statuses(dispatched).at(-1);
  assert.deepEqual([last.state, last.title, last.kind], ["live", "✳ Ionizing…", "run"]);
});

test("a dropped connection is offline since when it was lost; Reconnect is reconnecting, then live again", async () => {
  let clock = 1000;
  const { instance, dispatched, terms } = setup([], { now: () => clock });
  await flush();
  clock = 5000;
  terms.drop("s1");
  assert.equal(instance.state, "offline");
  assert.equal(statuses(dispatched).at(-1).lostAt, 5000);
  instance.reconnect();
  assert.equal(instance.state, "reconnecting", "a reconnect the person started, not a first attach");
  await flush();
  assert.equal(instance.state, "live");
  assert.equal(statuses(dispatched).at(-1).lostAt, null);
});

test("an exit ends it: read-only, the cursor hidden, the code reported", async () => {
  const { instance, screen, dispatched, terms } = setup();
  await flush();
  terms.exit("s1", 3);
  assert.equal(instance.state, "ended", "the drop that follows the exit does not make it offline");
  assert.equal(screen.cursorHidden, 1);
  assert.deepEqual([statuses(dispatched).at(-1).state, statuses(dispatched).at(-1).exitCode], ["ended", 3]);
});

test("mounts at the remembered text size; a new size refits at once, with one resize", async () => {
  const { instance, screen, terms } = setup([], { textSize: () => 15 });
  assert.equal(screen.options.fontSize, 15);
  await flush();
  screen.nextFit = { cols: 70, rows: 20 };
  instance.textSizeRequested({ detail: { px: 12 } });
  assert.deepEqual(screen.fontSizes, [12]);
  assert.deepEqual(resizesOf(terms), [{ type: "resize", size: { cols: 70, rows: 20 } }], "no wait for the debounce: the pane did not move");
});

test("Jump to latest scrolls to the bottom and never takes the focus; requests for another terminal are ignored", async () => {
  const { instance, screen } = setup();
  await flush();
  instance.jumpRequested({ detail: { id: "other" } });
  assert.equal(screen.bottomed, 0);
  instance.jumpRequested({ detail: { id: "s1" } });
  assert.equal(screen.bottomed, 1);
  assert.equal(screen.focused, 0, "F2: only a click on the screen, the Agent tab or Reconnect focuses it");
});

test("a Reconnect asked from the tab row focuses the terminal on the press (F2); for another terminal, nothing", async () => {
  const { instance, screen, terms } = setup();
  await flush();
  terms.drop("s1");
  instance.reconnectRequested({ detail: { id: "other" } });
  assert.equal(instance.state, "offline");
  instance.reconnectRequested({ detail: { id: "s1" } });
  assert.equal(instance.state, "reconnecting");
  assert.equal(screen.focused, 1);
});

test("scrolling up and new lines below are reported, for the tab row's Jump to latest", async () => {
  const { screen, dispatched } = setup();
  await flush();
  screen.input.onScrollState({ scrolledUp: true, newLines: 0 });
  screen.input.onScrollState({ scrolledUp: true, newLines: 12 });
  const last = statuses(dispatched).at(-1);
  assert.deepEqual([last.scrolledUp, last.newLines], [true, 12]);
});
