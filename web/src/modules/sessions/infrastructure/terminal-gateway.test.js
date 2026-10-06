import { assert, file, test } from "../../../shared/testing/test.js";
import { terminalGateway } from "./terminal-gateway.js";

file("sessions/infrastructure/terminal-gateway");

function fakeSockets() {
  const sockets = [];
  class FakeWebSocket {
    constructor(url) {
      this.url = url;
      this.readyState = 0;
      this.sent = [];
      sockets.push(this);
    }
    send(data) {
      this.sent.push(JSON.parse(data));
    }
    close() {
      this.readyState = 3;
      this.onclose?.({});
    }
    // test drivers
    open() {
      this.readyState = 1;
      this.onopen?.({});
    }
    text(message) {
      this.onmessage?.({ data: JSON.stringify(message) });
    }
    binary(text) {
      this.onmessage?.({ data: new TextEncoder().encode(text).buffer });
    }
    drop() {
      this.readyState = 3;
      this.onclose?.({});
    }
  }
  return { WebSocket: FakeWebSocket, latest: () => sockets.at(-1) };
}

function attach(location = { protocol: "http:", host: "127.0.0.1:8080" }) {
  const ws = fakeSockets();
  const seen = { snapshots: [], output: "", titles: [], exits: [], closed: 0, opened: 0 };
  const decoder = new TextDecoder();
  const conn = terminalGateway({ base: "/api", location, WebSocket: ws.WebSocket }).attach("s 1", {
    onOpen: () => seen.opened++,
    onSnapshot: (s) => seen.snapshots.push(s),
    onOutput: (bytes) => (seen.output += decoder.decode(bytes)),
    onTitle: (t) => seen.titles.push(t),
    onExit: (c) => seen.exits.push(c),
    onClosed: () => seen.closed++,
  });
  return { ws, seen, conn, socket: ws.latest() };
}

test("connects to the session's terminal socket on the page's origin", () => {
  assert.equal(attach().socket.url, "ws://127.0.0.1:8080/api/sessions/s%201/terminal");
  assert.equal(attach({ protocol: "https:", host: "harness.local" }).socket.url, "wss://harness.local/api/sessions/s%201/terminal");
});

test("an application run's terminal is another socket path; its log snapshot is marked", () => {
  const ws = fakeSockets();
  const snapshots = [];
  terminalGateway({
    base: "/api",
    path: (id) => `/runs/${encodeURIComponent(id)}/terminal`,
    location: { protocol: "http:", host: "h" },
    WebSocket: ws.WebSocket,
  }).attach("run-1", { onSnapshot: (s) => snapshots.push(s), onOutput: () => {} });
  const socket = ws.latest();
  assert.equal(socket.url, "ws://h/api/runs/run-1/terminal");
  socket.open();
  socket.text({ type: "snapshot", snapshot: { screen: "\x1b[32mok\x1b[0m\r\n", size: { cols: 80, rows: 24 }, log: true } });
  assert.equal(snapshots[0].log, true);
});

test("snapshot, output, title and exit arrive as the protocol sends them", () => {
  const { socket, seen } = attach();
  socket.open();
  socket.text({ type: "snapshot", snapshot: { screen: "hi", cursor_x: 2, cursor_y: 0, size: { cols: 100, rows: 30 }, title: "claude" } });
  socket.binary("more ");
  socket.binary("output");
  socket.text({ type: "title", title: "✳ working" });
  socket.text({ type: "exit", code: 3 });
  socket.drop();

  assert.equal(seen.opened, 1);
  assert.deepEqual(seen.snapshots, [{ screen: "hi", scrollback: "", cursorX: 2, cursorY: 0, altScreen: false, cols: 100, rows: 30, title: "claude", log: false }]);
  assert.equal(seen.output, "more output");
  assert.deepEqual(seen.titles, ["✳ working"]);
  assert.deepEqual(seen.exits, [3]);
  assert.equal(seen.closed, 1);
});

test("a snapshot's scrollback arrives with it; a server that sends none means no history", () => {
  const { socket, seen } = attach();
  socket.open();
  socket.text({ type: "snapshot", snapshot: { screen: "now", scrollback: "older\nold", size: { cols: 80, rows: 24 } } });
  socket.text({ type: "snapshot", snapshot: { screen: "now", size: { cols: 80, rows: 24 } } });
  assert.equal(seen.snapshots[0].scrollback, "older\nold");
  assert.equal(seen.snapshots[1].scrollback, "");
});

test("keys, pastes, resizes and resyncs go out as protocol messages", () => {
  const { socket, conn } = attach();
  socket.open();
  const press = (k, mods = {}) => conn.key({ key: k, shiftKey: false, altKey: false, ctrlKey: false, metaKey: false, ...mods });
  assert.equal(press("l"), true);
  assert.equal(press("c", { ctrlKey: true }), true);
  assert.equal(press("v", { metaKey: true }), false, "Cmd shortcuts stay with the browser");
  conn.paste("make check");
  conn.resize({ cols: 140, rows: 40 });
  conn.resync();
  assert.deepEqual(socket.sent, [
    { type: "key", key: { code: 108, text: "l" } },
    { type: "key", key: { code: 99, mod: 4 } },
    { type: "paste", text: "make check" },
    { type: "resize", size: { cols: 140, rows: 40 } },
    { type: "resync" },
  ]);
});

test("nothing is sent before the socket opens; closing on purpose is not a drop", () => {
  const { socket, conn, seen } = attach();
  conn.paste("early");
  assert.deepEqual(socket.sent, []);
  conn.close();
  assert.equal(seen.closed, 0);
});
