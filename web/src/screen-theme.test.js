import { FALLBACK, followTheme, plainColor, screenColors, screenFont } from "./screen-theme.js";
import { flush } from "./shared/testing/doubles.js";
import { assert, file, test } from "./shared/testing/test.js";

file("screen-theme");

// The form xterm.js parses by regex, with no canvas of its own.
const RGBA = /^rgba\(\d{1,3}, \d{1,3}, \d{1,3}, (0|1|0\.\d+)\)$/;
const root = document.documentElement; // the test page loads the design system, so the tokens resolve here
const ROLES = {
  background: "--color-sunken",
  foreground: "--color-ink",
  cursor: "--color-signal",
  cursorAccent: "--color-sunken",
  selectionBackground: "--color-selected",
};

const tokenOf = (el, name) => getComputedStyle(el).getPropertyValue(name).trim();

/** Runs fn with <html data-theme> pinned (or cleared when theme is null), then restores it. */
async function withPinned(theme, fn) {
  const before = root.dataset.theme;
  if (theme === null) delete root.dataset.theme;
  else root.dataset.theme = theme;
  try {
    await fn();
  } finally {
    if (before === undefined) delete root.dataset.theme;
    else root.dataset.theme = before;
  }
}

/** A matchMedia whose "(prefers-color-scheme: dark)" the test flips by hand. */
function fakeMedia(matches) {
  const listeners = new Set();
  const list = {
    matches,
    addEventListener: (_, fn) => listeners.add(fn),
    removeEventListener: (_, fn) => listeners.delete(fn),
  };
  return {
    matchMedia: () => list,
    listeners,
    flip() {
      list.matches = !list.matches;
      for (const fn of listeners) fn({ matches: list.matches });
    },
  };
}

test("every terminal color is the active token as a plain rgba color", () => {
  const colors = screenColors(root);
  for (const [role, token] of Object.entries(ROLES)) {
    assert.ok(RGBA.test(colors[role]), `${role} is rgba (got ${colors[role]})`);
    assert.equal(colors[role], plainColor(tokenOf(root, token)), role);
  }
  assert.equal(screenFont(root), tokenOf(root, "--font-mono"));
});

test("the selection keeps the token's translucency", () => {
  const alpha = Number(screenColors(root).selectionBackground.match(RGBA)[1]);
  assert.ok(alpha > 0 && alpha < 1, `selection alpha ${alpha}`);
});

test("pinning a theme on <html> changes the colors", async () => {
  await withPinned("light", async () => {
    const light = screenColors(root);
    await withPinned("dark", async () => {
      const dark = screenColors(root);
      assert.ok(light.background !== dark.background, "background differs between themes");
      assert.ok(light.foreground !== dark.foreground, "foreground differs between themes");
    });
  });
});

test("unreadable tokens fall back to the design-system colors of the same theme", () => {
  const bare = document.implementation.createHTMLDocument("bare").documentElement; // no stylesheet: every token is empty
  const light = screenColors(bare, { matchMedia: fakeMedia(false).matchMedia });
  const dark = screenColors(bare, { matchMedia: fakeMedia(true).matchMedia });
  assert.ok(RGBA.test(light.background), `fallback is rgba (got ${light.background})`);
  assert.equal(light.background, plainColor(FALLBACK.light.sunken));
  assert.equal(light.foreground, plainColor(FALLBACK.light.ink));
  assert.equal(light.cursor, plainColor(FALLBACK.light.signal));
  assert.equal(light.cursorAccent, plainColor(FALLBACK.light.sunken));
  assert.equal(light.selectionBackground, plainColor(FALLBACK.light.selected));
  assert.equal(dark.background, plainColor(FALLBACK.dark.sunken));
  assert.ok(light.background !== dark.background, "the OS preference picks the fallback theme");
  bare.dataset.theme = "light";
  assert.equal(screenColors(bare, { matchMedia: fakeMedia(true).matchMedia }).background, light.background, "a pin wins over the OS");
  assert.equal(screenFont(bare), FALLBACK.font);
});

test("the fallback colors are the tokens in design-system/tokens.json", async () => {
  const tokens = await (await fetch(new URL("../../design-system/tokens.json", import.meta.url))).json();
  for (const theme of ["light", "dark"]) {
    for (const role of ["sunken", "ink", "signal", "selected"]) {
      assert.equal(FALLBACK[theme][role], tokens.themes[theme].color[role], `${theme} ${role}`);
    }
  }
  assert.equal(FALLBACK.font, tokens.font.mono);
});

test("plainColor rejects what the browser cannot parse", () => {
  assert.equal(plainColor("not-a-color"), null);
  assert.equal(plainColor(""), null);
  assert.equal(plainColor(undefined), null);
  assert.equal(plainColor("#ff0000"), "rgba(255, 0, 0, 1)");
});

test("followTheme reports a pin, an unpin and an OS change, and stops when unsubscribed", async () => {
  const doc = document.implementation.createHTMLDocument("t");
  const media = fakeMedia(false);
  let changes = 0;
  const stop = followTheme(() => changes++, { root: doc.documentElement, matchMedia: media.matchMedia });

  doc.documentElement.dataset.theme = "dark";
  await flush();
  assert.equal(changes, 1, "a pin");
  delete doc.documentElement.dataset.theme;
  await flush();
  assert.equal(changes, 2, "an unpin");
  media.flip();
  assert.equal(changes, 3, "the OS preference");

  stop();
  doc.documentElement.dataset.theme = "light";
  media.flip();
  await flush();
  assert.equal(changes, 3, "nothing after stop");
  assert.equal(media.listeners.size, 0, "the media listener is removed");
});
