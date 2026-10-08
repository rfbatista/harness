import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTerminals } from "../../infrastructure/memory-terminals.js";
import { terminal } from "./terminal.js";

file("sessions/presentation/terminal");

function fakeScreen() {
  const screen = { drawn: [], written: "", size: { cols: 80, rows: 24 }, nextFit: null, focused: 0, disposed: false, input: null };
  const decoder = new TextDecoder();
  const create = (container, input) => {
    screen.input = input;
    return {
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
      size: () => screen.size,
      focus: () => screen.focused++,
      dispose: () => (screen.disposed = true),
    };
  };
  return { screen, create };
}

function setup(history = []) {
  const terms = memoryTerminals({ greeting: "claude is ready", history });
  const { screen, create } = fakeScreen();
  let resized = null;
  const el = document.createElement("div");
  const refs = { screen: document.createElement("div") };
  const mounted = mount(
    () => terminal({ terminals: terms.gateway, createScreen: create, observeResize: (_, cb) => ((resized = cb), () => {}) })("s1"),
    { el },
  );
  Object.defineProperty(mounted.instance, "$refs", { value: refs });
  mounted.instance.init();
  return { ...mounted, terms, screen, resize: () => resized() };
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

test("resizing the pane resizes the PTY", async () => {
  const { screen, terms, resize } = setup();
  await flush();
  screen.nextFit = { cols: 90, rows: 20 };
  resize();
  assert.deepEqual(terms.sent.get("s1").at(-1), { type: "resize", size: { cols: 90, rows: 20 } });
});

test("an exit is shown with its code; a drop offers to reconnect", async () => {
  const { instance, terms } = setup();
  await flush();
  terms.exit("s1", 2);
  assert.equal(instance.state, "exited");
  assert.equal(instance.stateWord, "exited (code 2)");
  assert.equal(instance.canReconnect, false);

  const other = setup();
  await flush();
  other.instance.state = "live";
  other.instance.connect(); // a fresh connection…
  other.instance.state = "closed"; // …that dropped
  assert.equal(other.instance.canReconnect, true);
});

test("output, titles, resizes and a reconnect's snapshot never move the focus; asking for it does", async () => {
  const { instance, screen, terms, resize } = setup();
  await flush();
  terms.print("s1", "more");
  terms.retitle("s1", "claude: tests");
  screen.nextFit = { cols: 90, rows: 20 };
  resize();
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
  instance.state = "closed";
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
