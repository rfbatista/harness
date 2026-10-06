// The terminal screen, on xterm.js: the one place the browser terminal
// library is used. It draws what the server's PTY prints and hands key
// presses and pastes back; it never interprets input itself (xterm's own
// onData is ignored), so the server's emulator stays the single source of
// truth for the terminal's modes. Its colors are the design tokens, taken at
// mount and again on every theme change (screen-theme.js), never xterm's own.

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";

import { followTheme, screenColors, screenFont } from "./screen-theme.js";

/** @type {import("./modules/sessions/presentation/components/terminal.js").CreateScreen} */
export function createScreen(container, { onKey, onPaste }) {
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: screenFont(),
    fontSize: 13,
    lineHeight: 1.15,
    scrollback: 2000,
    theme: screenColors(),
  });
  // A pinned theme restored after this screen mounted, a toggle click, the OS
  // flipping: each re-themes the screen in place.
  const stopFollowing = followTheme(() => {
    term.options.theme = screenColors();
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
      stopFollowing();
      container.removeEventListener("paste", paste, true);
      term.dispose();
    },
  };
}
