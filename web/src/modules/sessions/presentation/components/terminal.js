// A session's terminal: the PTY claude runs in on the server, drawn in the
// browser. The screen (xterm.js, injected as createScreen) draws; the gateway
// carries the snapshot, the output and the keys. Keyed on the session, so
// choosing another session mounts a fresh one and destroy() detaches.
//
//   <div x-data="sessionsTerminal(sid)" class="[ terminal ]">
//     <div class="[ screen ]" x-ref="screen"></div> …
//   </div>

/**
 * A screen draws a terminal and reports what is typed into it.
 * @typedef {object} Screen
 * @property {(snapshot: import("../../domain/ports.js").TerminalSnapshot) => void} draw  redraws from scratch
 * @property {(bytes: Uint8Array) => void} write
 * @property {() => ({ cols: number, rows: number } | null)} fit  fits the container; the new size, or null when unchanged
 * @property {() => { cols: number, rows: number }} size  the current size
 * @property {() => void} focus
 * @property {() => void} dispose
 *
 * @typedef {(container: HTMLElement, input: {
 *   onKey: (event: KeyboardEvent) => boolean,
 *   onPaste: (text: string) => void,
 * }) => Screen} CreateScreen
 */

/**
 * @param {{
 *   terminals: import("../../domain/ports.js").TerminalGateway,
 *   createScreen: CreateScreen,
 *   observeResize?: (el: HTMLElement, onResize: () => void) => () => void,
 * }} deps
 */
export const terminal = ({ terminals, createScreen, observeResize = resizeObserver }) => (sessionId = "") => {
  let screen = null;
  let connection = null;
  let stopObserving = () => {};

  return {
    sessionId,
    /** connecting → live → exited | closed */
    state: "connecting",
    exitCode: null,
    title: "",

    get stateWord() {
      switch (this.state) {
        case "live":
          return "live";
        case "exited":
          return `exited (code ${this.exitCode})`;
        case "closed":
          return "disconnected";
        default:
          return "connecting";
      }
    },
    get statusState() {
      return { live: "running", exited: "done", closed: "failed" }[this.state] ?? "idle";
    },
    get canReconnect() {
      return this.state === "closed";
    },

    init() {
      screen = createScreen(this.$refs.screen, {
        onKey: (event) => connection?.key(event) ?? false,
        onPaste: (text) => connection?.paste(text),
      });
      this.connect();
      stopObserving = observeResize(this.$refs.screen, () => this.fit());
    },

    connect() {
      this.state = "connecting";
      connection = terminals.attach(this.sessionId, {
        onOpen: () => {
          this.state = "live";
        },
        onSnapshot: (snapshot) => {
          screen.draw(snapshot);
          if (snapshot.title) this.title = snapshot.title;
          // The snapshot is at the server's size; take this pane's.
          this.fit(true);
          screen.focus();
        },
        onOutput: (bytes) => screen.write(bytes),
        onTitle: (title) => {
          this.title = title;
        },
        onExit: (code) => {
          this.state = "exited";
          this.exitCode = code;
          this.$dispatch("terminal-exited", { id: this.sessionId, code });
        },
        onClosed: () => {
          if (this.state !== "exited") this.state = "closed";
        },
      });
    },

    reconnect() {
      connection?.close();
      this.connect();
    },

    /** Fits the screen to the pane and tells the server; force sends even when unchanged. */
    fit(force = false) {
      if (!screen) return;
      const size = screen.fit() ?? (force ? screen.size() : null);
      if (size) connection?.resize(size);
    },

    focus() {
      screen?.focus();
    },

    destroy() {
      stopObserving();
      connection?.close();
      screen?.dispose();
    },
  };
};

function resizeObserver(el, onResize) {
  if (typeof ResizeObserver === "undefined") return () => {};
  let frame = 0;
  const observer = new ResizeObserver(() => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(onResize);
  });
  observer.observe(el);
  return () => observer.disconnect();
}
