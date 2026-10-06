---
name: Harness
description: A calm control room for a developer's live Claude agents, sessions and tickets.
colors:
  canvas: "oklch(0.985 0.003 190)"
  panel: "oklch(0.962 0.005 190)"
  raised: "oklch(1 0 0)"
  sunken: "oklch(0.94 0.006 190)"
  line: "oklch(0.9 0.006 190)"
  line-strong: "oklch(0.8 0.008 190)"
  line-control: "oklch(0.64 0.012 190)"
  ink: "oklch(0.22 0.01 190)"
  ink-muted: "oklch(0.45 0.012 190)"
  ink-faint: "oklch(0.53 0.012 190)"
  signal: "oklch(0.5 0.095 182)"
  signal-strong: "oklch(0.44 0.09 182)"
  signal-ink: "oklch(0.99 0.008 182)"
  attention: "oklch(0.52 0.12 65)"
  danger: "oklch(0.52 0.18 27)"
  danger-ink: "oklch(0.99 0.01 27)"
  success: "oklch(0.5 0.11 150)"
typography:
  headline:
    fontFamily: "Atkinson Hyperlegible Next, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.375rem"
    fontWeight: 700
    lineHeight: 1.25
  title:
    fontFamily: "Atkinson Hyperlegible Next, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 700
    lineHeight: 1.25
  title-sm:
    fontFamily: "Atkinson Hyperlegible Next, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 700
    lineHeight: 1.25
  body:
    fontFamily: "Atkinson Hyperlegible Next, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: 1.55
  label:
    fontFamily: "Atkinson Hyperlegible Next, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.8125rem"
    fontWeight: 500
    lineHeight: 1.4
  meta:
    fontFamily: "Atkinson Hyperlegible Next, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.75rem"
    fontWeight: 400
    lineHeight: 1.4
  mono:
    fontFamily: "Atkinson Hyperlegible Mono, ui-monospace, SF Mono, Menlo, monospace"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1.4
rounded:
  xs: "3px"
  sm: "5px"
  md: "8px"
spacing:
  3xs: "2px"
  2xs: "4px"
  xs: "6px"
  s: "8px"
  m: "12px"
  l: "16px"
  xl: "24px"
  2xl: "32px"
  3xl: "48px"
components:
  button-primary:
    backgroundColor: "{colors.signal}"
    textColor: "{colors.signal-ink}"
    typography: "{typography.label}"
    rounded: "{rounded.sm}"
    padding: "0 12px"
    height: "32px"
  button-secondary:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "0 12px"
    height: "32px"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.sm}"
    padding: "0 12px"
    height: "32px"
  button-danger:
    backgroundColor: "{colors.danger}"
    textColor: "{colors.danger-ink}"
    rounded: "{rounded.sm}"
    padding: "0 12px"
    height: "32px"
  input:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    padding: "0 8px"
    height: "32px"
  row:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    padding: "0 16px"
    height: "36px"
  nav-item:
    textColor: "{colors.ink-muted}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    padding: "0 8px"
    height: "32px"
  badge:
    textColor: "{colors.ink-muted}"
    typography: "{typography.meta}"
    rounded: "{rounded.xs}"
    padding: "0 6px"
    height: "20px"
  kbd:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.mono}"
    rounded: "{rounded.xs}"
    height: "20px"
---

# Design System: Harness

The CSS lives in `design-system/css/` (entry: `index.css`) and follows CUBE CSS: global → compositions → utilities → blocks, with exceptions as `data-*` / ARIA selectors inside each block. How to write and organise it is in [`design-system/README.md`](design-system/README.md); a rendered specimen of every block, with the Sessions screen as a worked example, is `design-system/specimen.html`. Tokens come from `design-system/tokens.json`, the single source of truth for both themes; the frontmatter here lists the **light** theme. In CSS every color role is `--color-<role>` (e.g. `--color-panel`) and has a matching `color-*` / `bg-*` utility where it makes sense.

## 1. Overview

**Creative North Star: "The Quiet Console"**

The harness is a room you glance into while agents work. Most of the time nothing needs you, and the screen should look like it: near-monochrome surfaces, small precise type, lists that read like good CLI output. Color is a signal, not a decoration. A teal dot means *an agent is working*. Amber means *it is waiting on you*. Red means *it broke*. Everything else, including things that finished successfully, recedes to neutral. When the screen does change, it is worth looking at.

Density comes from typography and well-made lists, not from panels. A screen has one job; a second pane appears only when the task needs it (the transcript beside the session list), never because there is room. The terminal heritage is felt through monospace transcripts, visible keyboard hints and a persistent stream bar, never through green-on-black cosplay. The system follows the OS light/dark preference and can be pinned with `<html data-theme>`.

This system rejects **busy IDE chrome**: dozens of panels, toolbars, icon rows and tabs competing for attention. It also rejects the old designer's Tailwind/daisyUI defaults (stock purple primary, pink secondary).

**Key Characteristics:**
- Restrained palette: tinted neutrals plus one accent (signal teal) on ≤ 10% of any screen.
- Status always pairs a dot with a word; color is never the only carrier.
- Flat, tonal layering; a shadow appears only on surfaces that float over others.
- Fixed rem type scale, 14px body, one sans and its mono sibling.
- Compositions (`flow`, `cluster`, `repel`, `with-sidebar`, `switcher`, `grid`, `wrapper`, `frame`) do the layout; native elements are styled globally; blocks stay small; state lives in `data-*` / ARIA attributes.
- Motion is 90–240ms ease-out, state-only, and collapses to instant under `prefers-reduced-motion`.

## 2. Colors

A cool, nearly colorless console with a trace of the signal hue (190) in every neutral, and four semantic voices that only speak when something is true.

### Primary
- **Signal Teal** (`signal`): the one accent. Running sessions, the primary action on a screen, current selection, links, focus rings. In dark mode it lifts to `oklch(0.78 0.1 182)`. Text on it uses `signal-ink` (5.5:1 light, 9.8:1 dark).

### Secondary
- **Waiting Amber** (`attention`): a session is waiting on the developer: input, a permission prompt, a review. Rows in this state also take the `attention-wash` background so they are findable in a peripheral glance.

### Tertiary
- **Fault Red** (`danger`): failed sessions, coded errors, destructive buttons, invalid fields.
- **Confirm Green** (`success`): a write was accepted. Transient only: toasts, a momentary check. Never a resting state on a list.

### Neutral
- **Console Canvas** (`canvas`): the app background and default row surface.
- **Rail Gray** (`panel`): sidebar, toolbars, list group headers, pane headers, stream bar. A second neutral layer that tells chrome apart from content.
- **Lifted White** (`raised`): fields, popovers, the command palette, toasts.
- **Well** (`sunken`): transcripts and code: where an agent's output is read.
- **Ink** (`ink`, 16.5:1), **Muted Ink** (`ink-muted`, 7:1), **Faint Ink** (`ink-faint`, ≥ 4.7:1): primary text, secondary text, and meta/placeholder text. All three clear AA on every surface in both themes.
- **Hairline** (`line`), **Strong Hairline** (`line-strong`), **Control Edge** (`line-control`, ≥ 3:1): dividers, emphasized dividers, input and secondary-button boundaries.

### Named Rules
**The Done Goes Quiet Rule.** A finished session is gray, not green. Success is the expected outcome; it earns no color on a resting list. Only running (signal), waiting (amber) and failed (red) are colored.

**The One Signal Rule.** Signal teal covers ≤ 10% of any screen. If two primary buttons sit side by side, one is wrong.

**The Word Beside the Dot Rule.** Every colored status ships with its word (`running`, `waiting`, `failed`) or an accessible name. A dot alone is allowed only where the word is adjacent in the same row.

## 3. Typography

**Body Font:** Atkinson Hyperlegible Next (with ui-sans-serif, system-ui)
**Label/Mono Font:** Atkinson Hyperlegible Mono (with ui-monospace, SF Mono, Menlo)

**Character:** One family designed for legibility at a glance. Its distinct letterforms (no confusable Il1, O0) are what a control room needs, and its mono sibling carries transcripts, paths, error codes and keys with the same voice. Self-host both as woff2; the stacks fall back cleanly to system fonts.

### Hierarchy
- **Headline** (700, 1.375rem/22px, 1.25): empty-state headlines only. There is no larger size in daily screens; 1.75rem is reserved for onboarding.
- **Title** (700, 1.125rem/18px, 1.25): the screen title (`h1`).
- **Title small** (700, 1rem/16px, 1.25): toolbar title, pane titles, `h2`.
- **Body** (400, 0.875rem/14px, 1.55): everything else: rows, controls, prose. Prose capped at 68ch.
- **Label** (500, 0.8125rem/13px, 1.4): field labels, dense rows, pane headers.
- **Meta** (400, 0.75rem/12px): timestamps, counts, list group headings, kbd, stream bar.
- **Mono** (400, 0.8125rem/13px, 1.4): transcripts, paths, IDs, error codes, diff stats.

Numbers are tabular everywhere (`font-variant-numeric: tabular-nums`), so durations and counts never jitter as they stream in.

### Named Rules
**The No Shouting Rule.** Nothing in the product is larger than 22px, and nothing is uppercase-tracked. Hierarchy comes from weight (400/500/700) and ink level, not size.

**The Mono Means Machine Rule.** Monospace marks text a machine produced or will parse: transcripts, paths, codes, keys. Never use it for decoration or headings.

## 4. Elevation

Flat by default. Depth is tonal: `panel` behind chrome, `canvas` for content, `sunken` for output wells, `raised` for anything you type into. Hairlines (`line`) separate regions; they are 1px, full-perimeter or single-edge dividers, never colored accents. A shadow exists only for surfaces that genuinely float above the page (command palette, toasts, popovers), and those also get a `line-strong` border, so the edge reads without the shadow.

### Shadow Vocabulary
- **Float** (`--shadow-float`; light: `0 1px 2px oklch(0.22 0.01 190 / 0.06), 0 8px 24px -4px oklch(0.22 0.01 190 / 0.14)`): command palette, toasts, popovers. Dark mode uses a deeper black-based variant.

### Named Rules
**The Only What Floats Rule.** If it does not overlap other content, it has no shadow. Panes, rows, buttons and inputs are flat.

## 5. Components

Familiar, exact, and keyboard-first. Every control shares one height (32px, or 26px for `data-size="sm"`), one radius (5px), one focus ring (2px signal, 2px offset).

### Buttons
- **Shape:** gently squared corners (5px), 32px high, 12px inline padding, 500 weight.
- **Primary** (`data-variant="primary"`): signal teal fill with `signal-ink` text. At most one per screen region.
- **Secondary** (default): `raised` fill, `line-control` edge, ink text.
- **Ghost** (`data-variant="ghost"`): no fill or edge, muted ink that brightens on hover. Toolbars and tertiary actions.
- **Danger** (`data-variant="danger"`): fault red fill. Destructive, irreversible actions only (stop session, delete).
- **States:** hover lays a 5–8% ink veil over the fill (90ms); active nudges 1px down; `:focus-visible` gets the shared ring; `disabled` / `aria-disabled` drop to 45% opacity; `aria-busy="true"` hides the label behind a spinner without changing width. Icon-only buttons use `data-icon` and need an `aria-label`.
- **Keyboard hints:** a `<kbd>` inside a button shows its shortcut; on filled variants it turns tonal.

### Chips (badges)
- **Style:** 20px high, 3px radius, 1px `line` border, meta type, muted ink. Neutral by default: tags, counts.
- **Tones:** `data-tone="signal|attention|danger"` swap in the role color plus its wash and a 35–40% tinted border. Add the `font-mono` utility for codes and diff stats.

### Cards / Containers
- **Pane** (`.pane`): 8px radius, 1px `line` border, `canvas` body, `panel` header 36px high. Used for a bounded region inside a screen (inspector, detail). Never nested inside another pane.
- **Internal padding:** 12px (`--space-m`).
- **Shadow strategy:** none (see Elevation).

### Inputs / Fields
- **Style:** `raised` fill, 1px `line-control` edge (≥ 3:1), 5px radius, 32px high, 8px inline padding. Styled globally on the native elements, so a bare `<input>`, `<select>` or `<textarea>` is already correct; add `font-mono text-sm` for paths, commands and replies to a session.
- **Focus:** border turns signal and gains a 1px signal halo (160ms).
- **Error:** `aria-invalid="true"` turns the edge fault red; the message sits below in the field's `.error`, linked by `aria-describedby`.
- **Disabled:** `panel` fill, faint ink.
- **Field anatomy:** label (13px, 500) → control → hint (12px, faint) or error (12px, red), 4px apart.

### Navigation
- **Top bar** (`.topbar`): a 44px `panel` strip across the app: the name, then the project picker, a native `<select>` (styled globally) in a GET form. The picker decides what the rail lists.
- **Rail** (`.nav`): 15rem `panel` column of the selected project's **tasks**, grouped under quiet 12px kanban headings (`in progress`, `review`, `todo`, `backlog`, `done`). A link is a truncating `.label`, then a status dot (`data-dot-only`: teal running, amber waiting on you) and a `.count` of live sessions.
- **Current:** `aria-current="page"` gets the `selected` wash and ink text. No stripes, no pills.
- **Inner list:** a task's own sessions sit in the page's `.split-view` list, beside the selected session's detail. A task can run several sessions at once.
- **Below 48rem:** the rail becomes a horizontally scrolling bar under the top bar; headings hide.

### List rows (signature)
The workhorse for sessions, tickets, agents and skills. Grid columns `status | title | meta` (override with `--row-columns`), 36px high, 16px inline padding, hairline dividers. Sticky `.list > .group` headers in `panel` (e.g. *Needs you / Running / Earlier*). Selection is `aria-selected="true"` (signal wash); rows waiting on the developer add `data-attention` (amber wash). Titles truncate; meta never wraps.

### Status (signature)
`.status[data-state]` draws an 8px dot plus its word: `running` (signal), `waiting` (amber, amber text), `failed` (red, red text), `done` (faint, filled), `idle` (hollow ring), `queued` (half-filled ring); `live` / `reconnecting` / `offline` for the stream. When a state arrives over SSE, set `data-signal` once: the dot emits a single 1.2s ring, never a loop. Under reduced motion it becomes a static outline.

### Stream bar (signature)
A 28px `panel` strip at the foot of the shell, `aria-live="polite"`: connection status, a one-line summary (*4 sessions · 1 waiting*), and the global keyboard hints (hidden below 48rem). It is how the developer knows that what they see is what the server holds.

### Terminal (signature)
A session's live PTY, drawn by xterm.js in the `sunken` well with the mono family at 13px; the theme is read from the color tokens (canvas converts their OKLCH). Under it, a 32px `panel` bar: the connection status (`live`, `exited (code N)`, `disconnected` with Reconnect) and the window title claude sets. Click anywhere on the screen to type into it.

### Artifact card and preview (Design tab)
The Design tab lists what a session published (`.artifact-card` rows in a `.list`: title, a neutral kind badge, the revision's note clamped to two lines, `rev N` and the time) beside `.preview`, which renders the selected one in the `sunken` well: a page or a dev server in an `<iframe sandbox="allow-scripts">` that fills the tab and scrolls inside, an image or video fitted and centred, a file as a download link with its name and size. Under it a 32px `panel` bar: the stream status word, the keyboard way out of the frame (Tab), and an "Open in a new tab" link. A card that arrives over the stream takes the one-shot arrival wash. Video never autoplays. The tab's label carries a count badge while publishes arrive behind another tab.

### Transcript (signature)
The `sunken` well where a session's output is read: mono 13px, a grid of `time | mark | text`. Line kinds via `data-kind`: `user` (signal mark ›), `agent` (·), `tool` (muted, ⌁), `error` (red, ×), `prompt` (amber wash, ?) for a permission request awaiting the developer.

### Command palette, toast, empty state, skeleton
- **Palette:** a native `<dialog>` 36rem wide, 12vh from the top, `raised` with `line-strong` border and Float shadow, opening in 240ms ease-out-expo. The input is 48px and 16px type; results are 32px items with right-aligned kbd or meta.
- **Toast:** bottom-right region, `raised`, Float shadow; a leading status dot centers on the first line.
- **Banner:** inline notice; full 1px border tinted from its tone, role wash fill, leading icon, and a mono `.code` line carrying the error `code` and next action.
- **Empty state:** left-aligned in a 30rem column: 22px headline, one sentence that teaches what the thing is, then the primary action and a ghost alternative.
- **Skeleton:** 0.75em bars with a slow linear sweep; static under reduced motion. Used in place of spinners inside content.

## 6. Do's and Don'ts

### Do:
- **Do** compose layout from the Every Layout primitives first, tune them with the `flow-space-*` / `gutter-*` utilities or their custom properties (`--sidebar-target`, `--switcher-threshold`, `--grid-min`), and keep blocks under 80–100 lines.
- **Do** express variants and state through `data-*` and ARIA attributes (`data-variant`, `data-state`, `aria-selected`, `aria-current`, `aria-invalid`, `aria-busy`) written inside the block's own file, never through modifier classes.
- **Do** read every color from a semantic role (`--color-ink-muted`, `--color-signal`, `--color-panel`); a raw `oklch()` outside `tokens.json` is a bug.
- **Do** pair every status color with its word, and announce meaningful session state changes through the stream bar's polite live region.
- **Do** show the error `code` and the next action whenever the API returns a `StructuredError`, in a `.banner` with its `.code` line.
- **Do** keep transitions between 90 and 240ms with `--ease-out`, and give every animation a reduced-motion fallback.
- **Do** keep text contrast ≥ 4.5:1 and control edges ≥ 3:1 in both themes; re-check when adding a surface.
- **Do** embed agent-produced pages only in `<iframe sandbox="allow-scripts">` and render their titles and notes as text; `allow-same-origin` is never added.

### Don't:
- **Don't** build **busy IDE chrome**: dozens of panels, toolbars, icon rows and tabs competing for attention. One job per screen; a second pane must earn its place.
- **Don't** bring back the old designer's Tailwind/daisyUI defaults: stock purple primary, pink secondary, generic cards.
- **Don't** color a finished session green on a resting list (The Done Goes Quiet Rule).
- **Don't** loop animations to show liveness. No pulsing dots, no breathing badges; the one-shot `data-signal` ring is the only status motion.
- **Don't** use `border-left` / `border-right` thicker than 1px as a colored accent on rows, banners, cards or callouts.
- **Don't** use gradient text, glassmorphism, neon glows, or green-on-black "hacker terminal" styling.
- **Don't** set anything above 22px or use uppercase tracked eyebrows; hierarchy is weight and ink.
- **Don't** add shadows to surfaces that don't float, or nest a `.pane` inside a `.pane`.
- **Don't** reach for a modal first. Inline editing and the side pane come before a dialog; the command palette is the one standing exception.
- **Don't** restyle native scrollbars, checkboxes or selects beyond theme color (`accent-color`, `scrollbar-color`).
