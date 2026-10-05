// The terminal screen, on xterm.js: the one place the browser terminal
// library is used. It draws what the server's PTY prints and hands key
// presses and pastes back; it never interprets input itself (xterm's own
// onData is ignored), so the server's emulator stays the single source of
// truth for the terminal's modes.

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";

/** @type {import("./modules/sessions/presentation/components/terminal.js").CreateScreen} */
export function createScreen(container, { onKey, onPaste }) {
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: token("--font-mono") || "ui-monospace, monospace",
    fontSize: 13,
    lineHeight: 1.15,
    scrollback: 2000,
    theme: theme(),
  });
  const fitter = new FitAddon();
  term.loadAddon(fitter);
  term.open(container);

  // Keys go to the server as KeyEvents. A key it does not take (Cmd shortcuts,
  // lone modifiers) is left to the browser, so copy still works.
  term.attachCustomKeyEventHandler((event) => {
    if (event.type !== "keydown") return false;
    if (onKey(event)) {
      event.preventDefault();
      return false;
    }
    return !event.metaKey;
  });

  // Pastes too: caught before xterm turns them into input.
  const paste = (event) => {
    const text = event.clipboardData?.getData("text") ?? "";
    event.preventDefault();
    event.stopImmediatePropagation();
    if (text) onPaste(text);
  };
  container.addEventListener("paste", paste, true);

  return {
    draw(snapshot) {
      term.reset();
      term.resize(Math.max(snapshot.cols, 2), Math.max(snapshot.rows, 1));
      if (snapshot.log) {
        // An application run's output so far: replayed as printed.
        term.write(snapshot.screen);
        return;
      }
      if (snapshot.altScreen) term.write("\x1b[?1049h");
      term.write(snapshot.screen.replace(/\r?\n/g, "\r\n"));
      term.write(`\x1b[${snapshot.cursorY + 1};${snapshot.cursorX + 1}H`);
    },
    write(bytes) {
      term.write(bytes);
    },
    fit() {
      const before = `${term.cols}x${term.rows}`;
      try {
        fitter.fit();
      } catch {
        return null; // not laid out yet
      }
      return `${term.cols}x${term.rows}` === before ? null : { cols: term.cols, rows: term.rows };
    },
    size: () => ({ cols: term.cols, rows: term.rows }),
    focus: () => term.focus(),
    dispose() {
      container.removeEventListener("paste", paste, true);
      term.dispose();
    },
  };
}

/** xterm wants plain colors; the design tokens are OKLCH, so let canvas convert them. */
function theme() {
  return {
    background: color("--color-sunken"),
    foreground: color("--color-ink"),
    cursor: color("--color-signal"),
    cursorAccent: color("--color-sunken"),
    selectionBackground: color("--color-selected"),
  };
}

function token(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function color(name) {
  const value = token(name);
  if (!value) return undefined;
  const ctx = document.createElement("canvas").getContext("2d");
  if (!ctx) return undefined;
  ctx.fillStyle = value;
  return ctx.fillStyle;
}
