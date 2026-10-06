// The terminal screen's theme: the design tokens the contract "design-system
// ↔ web terminal screen" names, as plain colors xterm.js can take, and the two
// ways the theme changes (a pinned <html data-theme>, the OS preference).
// No xterm here, so it is tested without one.

/**
 * The same tokens, as design-system/tokens.json writes them, for when a token
 * cannot be read (no stylesheet yet, a detached document). Never xterm's
 * black-on-white default. A test pins these to tokens.json.
 */
export const FALLBACK = {
  light: {
    sunken: "oklch(0.94 0.006 190)",
    ink: "oklch(0.22 0.01 190)",
    signal: "oklch(0.5 0.095 182)",
    selected: "oklch(0.5 0.095 182 / 0.1)",
  },
  dark: {
    sunken: "oklch(0.145 0.006 190)",
    ink: "oklch(0.94 0.006 190)",
    signal: "oklch(0.78 0.1 182)",
    selected: "oklch(0.78 0.1 182 / 0.12)",
  },
  font: '"Atkinson Hyperlegible Mono", ui-monospace, "SF Mono", Menlo, Consolas, monospace',
};

const DARK_QUERY = "(prefers-color-scheme: dark)";

const defaultMatchMedia = (query) => globalThis.matchMedia?.(query) ?? null;

/**
 * The terminal's colors from the active tokens on `root`, as rgba strings.
 * @param {HTMLElement} [root]
 * @param {{ matchMedia?: (query: string) => { matches: boolean } | null }} [options]
 */
export function screenColors(root = document.documentElement, { matchMedia = defaultMatchMedia } = {}) {
  const fallback = FALLBACK[prefersDark(root, matchMedia) ? "dark" : "light"];
  const color = (name, role) => plainColor(token(root, name)) ?? plainColor(fallback[role]);
  return {
    background: color("--color-sunken", "sunken"),
    foreground: color("--color-ink", "ink"),
    cursor: color("--color-signal", "signal"),
    cursorAccent: color("--color-sunken", "sunken"),
    selectionBackground: color("--color-selected", "selected"),
  };
}

/** The mono font stack. @param {HTMLElement} [root] */
export function screenFont(root = document.documentElement) {
  return token(root, "--font-mono") || FALLBACK.font;
}

/**
 * Calls onChange whenever the theme changes: data-theme set or removed on
 * root, or the OS preference flipping. Returns a function that stops both.
 * @param {() => void} onChange
 * @param {{ root?: HTMLElement, matchMedia?: (query: string) => EventTarget & { matches: boolean } | null }} [options]
 */
export function followTheme(onChange, { root = document.documentElement, matchMedia = defaultMatchMedia } = {}) {
  const observer = new MutationObserver(() => onChange());
  observer.observe(root, { attributes: true, attributeFilter: ["data-theme"] });
  const media = matchMedia(DARK_QUERY);
  const onMedia = () => onChange();
  media?.addEventListener?.("change", onMedia);
  return () => {
    observer.disconnect();
    media?.removeEventListener?.("change", onMedia);
  };
}

function prefersDark(root, matchMedia) {
  const pinned = root.dataset.theme;
  if (pinned === "dark" || pinned === "light") return pinned === "dark";
  return matchMedia(DARK_QUERY)?.matches ?? false;
}

function token(root, name) {
  const view = root.ownerDocument.defaultView ?? globalThis;
  return view.getComputedStyle(root).getPropertyValue(name).trim();
}

// A 1×1 canvas converts whatever color syntax the browser knows (the tokens
// are OKLCH) into the rgba() form xterm parses itself. The litmus color
// tells an unparsable value apart: the browser leaves fillStyle unchanged.
const LITMUS = "#010203";
let ctx = null;

/**
 * @param {string | undefined} css
 * @returns {string | null} `rgba(r, g, b, a)`, or null when the browser cannot parse css
 */
export function plainColor(css) {
  if (!css) return null;
  ctx ??= document.createElement("canvas").getContext("2d", { willReadFrequently: true });
  if (!ctx) return null;
  ctx.fillStyle = LITMUS;
  ctx.fillStyle = css;
  if (ctx.fillStyle === LITMUS && css.toLowerCase() !== LITMUS) return null;
  ctx.clearRect(0, 0, 1, 1);
  ctx.fillRect(0, 0, 1, 1);
  const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
  return `rgba(${r}, ${g}, ${b}, ${Math.round((a / 255) * 1000) / 1000})`;
}
