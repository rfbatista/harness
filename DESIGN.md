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
  attention-strong: "oklch(0.46 0.11 60)"
  danger: "oklch(0.52 0.18 27)"
  danger-ink: "oklch(0.99 0.01 27)"
  success: "oklch(0.5 0.11 150)"
  scrim-strong: "oklch(0.1 0.005 190 / 0.82)"
  code: "oklch(0.975 0.004 190)"
  line-number: "oklch(0.5 0.012 190)"
  diff-add: "oklch(0.46 0.11 150)"
  diff-del: "oklch(0.48 0.17 27)"
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
- **Waiting Amber** (`attention`): a session is waiting on the developer: input, a permission prompt, a review. Rows in this state also take the `attention-wash` background so they are findable in a peripheral glance. Amber text on a wash or a badge uses `attention-strong`.

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
- **Dim Scrim** (`scrim`) and **Dark Scrim** (`scrim-strong`): what sits behind a modal surface. `scrim` dims the page behind a sheet or dialog; `scrim-strong` (near-black at 82%, 86% in dark mode) sits behind an image in the Lightbox, where the page must drop away.

### Code
Reading code has its own small palette, used only inside the code blocks (§5 Code views). Every text colour clears 4.5:1 on the code well and on the add, remove and target washes in both themes. Run `python3 -I design/code-views/tools/contrast.py design-system/tokens.json` after changing one.
- **Code Well** (`code`): where files and diffs are read. In light it is a shade lighter than `sunken` (0.975), so nine syntax colours and two washes all clear AA. In dark it is `sunken`.
- **Line Number** (`line-number`): the gutter. It is not `ink-faint`, which drops to 4.3:1 on the light washes.
- **Syntax** (`tok-comment`, `tok-string`, `tok-number`, `tok-keyword`, `tok-type`, `tok-function`, `tok-tag`, `tok-attr`, `tok-regexp`): quiet, mid-chroma hues at one lightness per theme (L ≈ 0.48 light, ≈ 0.8 dark). Comments are neutral and italic. Variables stay ink, and punctuation, operators and meta stay muted ink.
- **Diff** (`diff-add`, `diff-del` for the signs, counts and A/D marks; `diff-add-wash`, `diff-del-wash` for line backgrounds; `target-wash` for a line targeted from the URL). The signs are separate from `success` and `danger`, which fall under 4.5:1 on the washes.
- **Context hues** (`context-0`…`context-7`): the categorical colours of bounded contexts, as graphics only (swatches and bars, ≥ 3:1 on canvas and panel). They avoid the diff red and green and the teal signal. A context's hue is its index in the code map, mod 8 (`data-hue`, utilities/context-hue.css).

Lowest measured ratios: light `line-number` and `tok-comment` 4.86 on the target wash, dark `line-number` 5.50 on the target wash, light `context-7` 3.30 on panel. The full table is in the task document *Plan: Design system — code views* §3.2.1.

### Named Rules
**The Done Goes Quiet Rule.** A finished session is gray, not green. Success is the expected outcome; it earns no color on a resting list. Only running (signal), waiting (amber) and failed (red) are colored.

**The One Signal Rule.** Signal teal covers ≤ 10% of any screen. If two primary buttons sit side by side, one is wrong.

**The Letter Beside the Colour Rule.** In code, a change is a letter (A M D R C T U), a diff line is a sign (+ −), a zone is its name. Colour only repeats them.

**The Word Beside the Dot Rule.** Every colored status ships with its word (`running`, `waiting`, `failed`) or an accessible name. A dot alone is allowed only where the word is adjacent in the same row. Session rows (`.row[data-layout="session"]`) are a recorded exception (ADR 0029): they show a dot only for running and done, with the word in the group heading and in a visually hidden label; uncommon states keep a visible status chip.

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
- **States:** hover lays a 5–8% ink veil over the fill (90ms); active nudges 1px down; `:focus-visible` gets the shared ring; `disabled` / `aria-disabled` drop to 45% opacity; `aria-busy="true"` slides a spinner in beside the label over 240ms, so the label stays readable and the button widens smoothly; on `data-icon` buttons the spinner replaces the icon instead. Icon-only buttons use `data-icon` and need an `aria-label`.
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
- **Selects:** the native `<select>` gets a drawn 6px chevron (two token-coloured gradients, so it follows the theme) and three exceptions: `data-size="sm"` (26px), `data-fit` (as wide as its value) and `data-variant="ghost"` (no edge or fill until hover or focus, for cards, rows and toolbars). A chosen `<option value="">` reads as a placeholder in faint ink; `aria-busy="true"` with `disabled` and a hint says options are loading. See Dropdowns for when a select is not enough.
- **Radio and checkbox groups:** a `<fieldset class="field">` whose `<legend>` reads like a label, one `.field[data-inline]` row per choice.

### Dropdowns
Choosing from a list comes in five variants, lightest first. **Which one:** ask in order and take the first yes; when in doubt go lighter (a Select you can upgrade later beats a Combobox nobody needed).

1. Does picking run a command (delete, copy link, open, move) instead of setting a value the page keeps? → **Menu button** (`x-data="menu"`).
2. Is picking a step of its own, opened from a button, over a large set (attach an asset to a task, jump to a session)? → **Picker dialog** (`picker()` in the palette `<dialog>`).
3. 2–5 options worth comparing at a glance, or several values from 6 or fewer? → **Not a dropdown:** a radio or checkbox group in a fieldset.
4. More than ~15 options, a name the person would type, options loaded per query, several values, or a new value typed in? → **Combobox** (`x-data="combobox"`).
5. Options that need a second line, a status dot or badge, or a clearable value? → **Listbox select** (`x-data="dropdown"`).
6. Otherwise, a short list of plain-text values, and every templ page that must work without JavaScript → **Select** (a bare `<select>`).

Every control has a visible label (or a visually hidden one when the context names it), and a value control never runs a destructive action on change.

- **One closed look:** the Select, the Listbox select's trigger and the Combobox's field are the same box (see Inputs): a person cannot tell the variant until it opens. The chevron turns up while open. The scripted variants wrap a native `<select>`, which stays the source of truth (`change`, `x-model`, form submit, the no-JS page); a query-loaded Combobox names a method on its scope with `data-source` and carries its value in a hidden input.
- **The popup** (`.popup`) floats like the palette: `raised`, `line-strong` edge, 8px radius, Float shadow, 4px from the trigger, at least its width and at most 24rem, about 8½ rows tall so the half row says it scrolls. It lives in the top layer, flips above when the room below is short, fades and slides 4px in 160ms, and only one is open at a time. Where `appearance: base-select` is supported the Select's own open list takes the same surface, automatically and with plain-text rows.
- **Options** (`option.css`, the anatomy the palette's Picker dialog shares): 32px rows, `ink-muted` text, columns lead (16px status dot) · label with an optional faint second line · meta (a word, badge or kbd) · check. **Active** (keys or pointer on it, `data-active`): the hover veil, ink text and an inset `line-strong` ring. **Selected**: the `selected` wash and a drawn signal check; the label never turns bold. Signal marks the chosen row only. In forced colours the active row gets a Highlight outline and the check is CanvasText. Several values swap the check for a leading 14px box. Disabled options stay readable at 45% and are skipped by the arrows.
- **Combobox extras:** the match is bold ink with a 1px underline, never a colour alone; a pinned option (`data-pinned`) stays on top whatever the filter; free text (`data-free-text`) adds a last row "Use “…”" (or `data-free-text-label`); several values show as 22px `.chip`s (two rows at rest, then "+N more") with a footer count and Clear all.
- **Messages:** empty, no results and loading words are a `text-sm` muted sentence in the popup; loading shows three skeleton rows (and a spinner in a Combobox's field); an error is a danger sentence with a small Retry (`data-error`, `dropdown-retry`).
- **Menu:** 32px items in ink with a muted icon and a right-aligned kbd or faint reason ("not running"); destructive items last, after a separator, in danger ink with a `danger-wash` hover and "…" when they confirm; a `menuitemradio` shows a 6px signal dot where the thing is now. The trigger is an ordinary `.button` with a small chevron, or a ghost icon button named for its object; open, it shows the pressed veil.
- **Phones (below 48rem):** the scripted variants open as bottom sheets: full width, 8px top corners, scrim behind, at most 72dvh, a 44px header with the field's label and Close or Done, 44px rows. The Combobox's tall sheet starts 8% from the top with its input first; the menu becomes an action sheet titled with its object. Small triggers keep their look but get a 44px hit area on coarse pointers.
- **Keys:** each variant follows its WAI-ARIA APG pattern (select-only combobox, editable combobox with list autocomplete, menu button). A closed dropdown never takes the app's global keys; an open one keeps every key, and Esc closes it before any pane or dialog around it.

### Navigation
- **Top bar** (`.topbar`): a 44px `panel` strip across the app: the name, then the project picker, a small fitted native `<select>` in a GET form. The picker decides what the rail lists.
- **Rail** (`.rail`): 15rem `panel` column: a skip link, one full-width **New task** button, the **Project** nav (Board, Review requests with its amber count, Documents, Design assets) closed by a hairline, then the **Tasks** nav: "Tasks N" with the Needs you toggle, a filter field with a `/` hint, and the five status groups in rail order (In progress, Review, Todo, Backlog, Done), always present.
- **Rows** (`.nav a`): 28px (`--size-nav-row`; 44px on a coarse pointer and in the drawer), grid `16px slot · label · meta`. The slot holds an icon or the status dot (teal running, amber waiting). Meta is one item, by priority: the amber reviews badge, "waiting" in `attention-strong`, "N live" in faint ink. Rows waiting on the person take the `attention-wash`. Titles truncate on one line; the full title is a native tooltip.
- **Groups** (`.nav .group`): a sticky 12px bold heading (`h3`) in sentence case with a chevron and a count, a disclosure button that folds the group. Folded groups still show the open task and what needs the person (the peek rule), and the server paints them folded from the `rail_collapsed` cookie. Done shows its five latest, then "Show N more".
- **Current:** `aria-current="page"` gets the `selected` wash and ink text, on the open task (two lines) and on the current project page. No stripes, no pills.
- **Inner list:** a task's own sessions sit in the page's `.split-view` list, beside the selected session's detail. A task can run several sessions at once. `data-expanded` hides the list and gives the detail the whole width at any viewport (the Code tab's Expand, ⇧E). It switches at once, with no animation.
- **Below 48rem:** the rail becomes a drawer from the inline-start edge (`raised`, Float shadow, scrim), opened by the "Tasks" button first in the top bar, with 44px rows.

### List rows (signature)
The workhorse for sessions, tickets, agents and skills. Grid columns `status | title | meta` (override with `--row-columns`), 36px high, 16px inline padding, hairline dividers. Sticky `.list > .group` headers in `panel` (e.g. *Needs you / Running / Earlier*). Selection is `aria-selected="true"` (signal wash); rows waiting on the developer add `data-attention` (amber wash). Titles truncate; meta never wraps. In a narrow list (a task's sessions beside their detail) rows take `data-stacked`: status, title and a row action (`.action`, e.g. Resume) on the first line, the meta under the title on the second, both truncating, so the action is never pushed out.

### Task context (task page title row)
The task page's vertical stack holds only the agents: the title row, then the session list and the session pane (ADR 0042). What the old context strip showed hangs from the title row as popovers, in the top layer, so it costs the agents nothing; each is a ghost button with a count, opens a `.popup` (`role="dialog"`, `popover="manual"`) under the button on the task note's surface, and closes with Esc, *Close* or a press outside, the focus returning to the button. None is ever stored open: a page loads with every one closed. Below 48rem each is a bottom sheet with a scrim (`data-sheet`). The title row's order is *Feature*, *Reviews*, *Description*, *Documents*, *Comments*, *Edit*, *New session*, then the ⋯ task menu.

**Reviews popover.** A ghost *Reviews* button with an amber count, present only while a request waits (the server paints it from the task's pending count; the page follows the feed). It opens `.context-popover` (34rem wide, at most `min(70dvh, 40rem)` tall, the head and the foot stay, the body scrolls) with every waiting request as a card, newest first: subject, who and when, the full text, the documents and artifacts it names, *Approve* (primary on the first card) and *Request changes…*, which opens a note field with *Send*. A card leaves once its answer is confirmed and the popover closes itself after the last one. A request withdrawn while the person writes stays, read-only, until dismissed. The foot counts the settled requests and links to the project's Review requests page. A link to `#reviews` or `#review-{id}` opens it. Below 48rem the session head's back button carries an amber dot (`data-dot`) and the name *Sessions, N reviews waiting* while a request waits, because the title row is hidden with a session open.

**Feature popover.** On a feature task only, a ghost *Feature* button with the children count, first among the actions. The popover (`.context-popover`, 32rem) pins the PRD (title, *Open ↗*) over the child tasks as one-line `.row`s (status word with its dot, title link, *N docs*) in review, in progress, todo, backlog, done order; the foot summarises the statuses (*3 in review · 2 in progress · 11 todo*) and links to the board. It follows the feed. A link to `#feature` opens it.

**Token savers (Edit form).** The task-level Token savers control is the third section of the task's Edit form, under Title and Description (`.task-savers`): a row per saver (rtk, caveman, codegraph) with an inherit / on / off select and the server's line on what it resolves to and from which level, under the sentence that a change reaches sessions started afterwards. Choices are held in the form and written on *Save*, one activation per changed saver after the task itself; *Cancel* drops them; a refused write leaves the form open with its error. The form scrolls inside the popover while *Save* and *Cancel* stay in reach (`.note-actions`, sticky).

**Task menu.** One ghost ⋯ (`aria-label="More task actions"`) at the end of the title row, the session ⋯ menu's Menu button pattern (an action sheet below 48rem): *Design assets* and *Resources* with their live counts, then a separator and *Delete task*, which keeps its confirmation.

**Task note.** The task's description never sits in the vertical stack. The title row's first action is a ghost *Description* button, shown only when the task has a description (`aria-haspopup="dialog"`, `aria-expanded`, `aria-controls`). It opens a popover in the top layer (`.popup.task-note`, `role="dialog"`, `popover="manual"`) under the button: a head with the title, *Edit* and *Close*, and a `prose` body that scrolls inside, 36rem wide and at most `min(70dvh, 40rem)` tall. Below 48rem the same popup takes `data-sheet` (a bottom sheet with a scrim). Esc, *Close* or a press outside closes it and the focus returns to the button; the open state is never stored, so a page always loads with it closed. *Edit* (in the head or in the title row) shows the task's Edit form, with the magic pens, in the same popover; *Save* closes it. A task with no description has no button, and the title row's *Edit* opens the form directly. The status-change line stays under the toolbar.

### Board (project root)
`.board` lays the project's tasks out as kanban columns (`.column`, `panel` surface, 8px radius) in board order: Backlog, Todo, In progress, Review, Done, every column present even when empty. Each `.card` (`canvas` surface, 5px radius) is the task's title as a link plus a `.meta` row carrying the rail's dot and live-session count, and a ghost, small, fitted native `<select>` that moves the task. A card that moves or arrives over the feed takes `data-fresh`: the one-shot arrival wash, a static inset line under reduced motion. The board never scrolls the page: it scrolls horizontally inside the screen's scroll region.

### Status (signature)
`.status[data-state]` draws an 8px dot plus its word: `running` (signal), `waiting` (amber, amber text), `failed` (red, red text), `done` (faint, filled), `idle` (hollow ring), `queued` (half-filled ring); `live` / `reconnecting` / `offline` for the stream. When a state arrives over SSE, set `data-signal` once: the dot emits a single 1.2s ring, never a loop. Under reduced motion it becomes a static outline.

### Stream bar (signature)
A 28px `panel` strip at the foot of the shell, `aria-live="polite"`: connection status, a one-line summary (*4 sessions · 1 waiting*), and the global keyboard hints (hidden below 48rem). It is how the developer knows that what they see is what the server holds.

### Terminal (signature)
A session's live PTY, drawn by xterm.js in the `sunken` well with the mono family at 13px (text size Smaller 12 / Default 13 / Larger 15, remembered per browser); the theme is read from the color tokens (canvas converts their OKLCH). `.screen` holds the gutter; its unpadded `.host` is what xterm fits whole rows and columns to, so the last row (the agent's status line) is always whole. Click anywhere on the screen to type into it. In a session's pane it has no bar under it: the connection and the tools sit at the end of the tab row (below). States: *connecting* (a word in the empty well), *reconnecting* / *offline* (the last screen stays, dimmed to 55%), *ended* (the last screen, read-only, under a `.pane-note` strip "This session ended." + Resume session; `danger` tone when it failed), and *waiting on you* (a 3px amber edge in the gutter, never over text). Keyboard focus shows as a hairline inside the well. The App tab's run terminal keeps the 32px `panel` bar until it gets the tools.

### Session screen
A session's detail column (`.session`) is fixed to the viewport: a 52px **session head**, a 40px **tab row**, then the terminal down to the stream bar, the page's only footer. Nothing else takes or changes its height, so the terminal resizes only when the window, the rail, the text size or focus mode changes it.
- **Session head** (`.session-head`): one row, never taller: state, title (truncates last), agent · age (hide first), then the PR badge, the Checks button, Git history (moves into ⋯ on a narrow column), Stop, and ⋯. An ended or failed session keeps the same shape; its Resume is on the pane's note. In narrow mode (below 64rem, the list hidden) a ‹ back button leads. On a phone it is two short lines (‹ · title · ⋯ over state · agent · age · checks) and every action is in the ⋯ sheet, 44px rows.
- **Checks button** (`.check-button`): a delegate's status-check loop as one ghost button, "● Checks · 20m next 15m ▾" (dot `live` on, `idle` paused, `failed` after a failed change). It opens the **check panel**: the loop in words, *Check every* (the native Select), a hint, Pause / Resume in the footer, and an inline error with Retry.
- **⋯ menu** (the Menu block): Copy session id, Git history when collapsed, Delete session… (danger). Delete asks in a **confirm popup** anchored to ⋯ in the top layer (title in danger, the consequence, Keep it / Delete session); it traps focus, Esc returns to ⋯. On a phone it is a sheet.
- **Tab row** (`.pane-tabs`): the tabs, then the open terminal's **tools**: the connection as a quiet dot while live (the window title in its tooltip); while connecting, reconnecting or offline the tools collapse to the word and Reconnect. Jump to latest appears only while scrolled up, in the `signal` wash with a count ("↓ 12 new lines"); it scrolls and never takes focus. Text size (a menu) and focus mode (a toggle). Design and Conversation have no tools. On a phone the tabs are 44px and only the dot (and Reconnect) stay; text size moves to the ⋯ sheet.
- **Focus mode** (`app-shell[data-focus]`): the rail, the page toolbar, the list and the head step aside; the tab row leads with the title and has *Exit focus*. Never bound to Esc, which belongs to the agent.

### Artifact card and preview (Design tab)
The Design tab lists what a session published (`.artifact-card` rows in a `.list`: title, a neutral kind badge, the revision's note clamped to two lines, `rev N` and the time) beside `.preview`, which renders the selected one in the `sunken` well: a page or a dev server in an `<iframe sandbox="allow-scripts">` that fills the tab and scrolls inside, an image or video fitted and centred, a file as a download link with its name and size. Under it a 32px `panel` bar: the stream status word, the keyboard way out of the frame (Tab), and an "Open in a new tab" link. A card that arrives over the stream takes the one-shot arrival wash. Video never autoplays. The tab's label carries a count badge while publishes arrive behind another tab.

### Resources and comments (task)
What a person adds to a task for context: **resources** (files, images, audio, links) and plain-text **comments**. Never call them "attachments"; that word belongs to project design assets attached to a task. They live on a task sub-page, *Resources and comments*, reached from two ghost links in the task toolbar (*Resources N*, *Comments N*, beside Documents and Design assets). From 64rem the Resources column (21–27rem) sits beside the Comments thread, each scrolling under a 44px `panel` section head, with the composer pinned at the foot of the thread. Below 64rem it is one column: resources, then comments, then the composer. The page toolbar keeps `flex-shrink: 0`. The blocks below are specified in the project asset *Blocks — resource row, drop target, comment, composer, lightbox* and in the task document *Design: task resources and comments*.
- **Resource row** (`.resource` in a `.resources-list`): a 40px `sunken` tile, the title (it opens the resource) over a faint meta line (*PNG image · 403 KB · 3 h ago*), then a ghost Download icon button and a ⋯ Menu button named for the resource. By kind:
  - **image:** the tile is the image itself and opens the Lightbox. `image/svg+xml` has no preview and downloads like a file.
  - **audio:** a native `<audio controls>` spans the row under the title.
  - **PDF and text/plain:** open in a new tab.
  - **other files:** download; the tile shows the extension in mono.
  - **link:** opens in a new tab (`noopener noreferrer`); the address shows in mono.
- **Row states:**
  - `data-state="uploading"`: a 4px determinate `<progress>` and Cancel.
  - `data-state="failed"`: danger wash, warning tile, message and `.code` line. A file over 25 MiB is refused before sending.
  - `data-editing`: rename in place.
  - `data-confirming`: delete asks in the row, *Delete* and *Keep it*.
  - `data-fresh`: the one-shot arrival wash.
- **Drop target** (`.drop-target`, `.drop-hint`): files dragged over the page arm it with a dashed `line-control` outline (`data-drop-armed`). Over it, `data-drop-over` shows a 2px dashed signal outline and an overlay naming what a drop does. The quiet hint row says what to do, worded for coarse pointers on touch screens. Drag and paste are shortcuts only; the Add files button is the full path.
- **Comment** (`.comment` in an `<ol class="thread">`): oldest first, no bubbles and no avatars. A header reads *You · 3 h ago · edited*, with ghost Edit and Delete buttons. The body is escaped plain text, `pre-wrap`, capped at 68ch. Edit and delete happen in place.
- **Composer** (`.composer`): a textarea that grows with its content, a hint saying running sessions get each new comment as a message and edits are not sent, a counter from 9,000 of the 10,000 characters (amber, then red), and a primary *Comment ⌘↵*.
- **Lightbox** (`dialog.lightbox`): the one modal on the page. It shows the image fitted over a near-black scrim (`scrim-strong`), with ← → between the task's images, and a `raised` footer with the name, *n of N*, Open in a new tab, Download and Close. Focus returns to the control that opened it.
- **Copy:** adding says *Sessions running on this task are told about it.* Edits, renames and deletes never claim to tell anyone.

### Notifications (top bar and inbox)
What needs the person across every project. **The bell** ends the `topbar`: a ghost icon button with an `attention` badge for the unread count. The badge is hidden at 0 and reads *99+* past 99. The accessible name keeps the exact count (*Notifications, 3 unread*). The server paints the count, so it is right before any script runs. The document title leads with it too: *(3) Task · harness*. On a phone the bell stays on the first line, beside the Tasks button. Without JavaScript the bell is a link to the inbox.
- **Panel** (`.popup.notifications-panel`): a disclosure, not a menu. The bell has `aria-expanded`, and the panel is a non-modal region of links and buttons on the shared popup surface, 26rem wide. It shows the latest 20 notifications. Its head has an *All / Unread* switch. Its foot has *Mark all read*, *Desktop alerts* and *See all notifications*. Esc closes it and returns focus to the bell. Below 48rem it is the tall sheet.
- **Row** (`.notification` in a `.notification-list`): a marker, the title as a link, a meta line, the plain-text body and a ghost *Mark read* button. The meta line reads *project · task*, then the level, then the age on the right. `data-level="action"` adds the amber rule and wash and the *Needs you* label; `info` is neutral. `data-state="unread"` is a filled dot and a bold title. `read` drops the dot. `settled` is muted and says *Settled*. The panel clamps the body to two lines and the page shows it whole. In forced colours, the dot and the rule keep their shape.
- **Inbox page** (`/notifications`, `.inbox`): filters in a GET form: a native project select and an *All / Unread* radio pair, not a dropdown. Then the list and *Load more*. *Mark all read* names its scope (*Mark all read in harness*).
- **Live and alerts:** the stream bar's polite region announces only a new `action` item (*New in harness: Review requested…*). A read or a resolve is never announced. Desktop alerts are opt-in from the panel's switch: the permission is asked only in that click, and an alert fires only while the tab is hidden.

### Usage (project)
A project's token usage at `/projects/{p}/usage`, on the rail's Project nav. The toolbar has a `segmented` range switch: *Today / 7 days / 30 days*. **Tiles** (`.usage-tiles`) show total, input, output, cache write, cache read, requests and the cost. The cost is always labelled *Estimated cost*. An unpriced cost is *—*, never *$0*, and a note says what the estimate leaves out. **Tokens over time** (`.usage-chart`) is an SVG of stacked bars, one per hour or day across the whole range, empty ones included. Each column has a hover title, and *Show as a table* gives the same numbers as text. **Series colours** are the eight validated categorical slots on `.usage` (`--series-1`…`8`, with their own dark steps). A model keeps its slot for the page's life, and a ninth model folds into *Other*, which is neutral ink. Text never takes a series colour. **Top tasks / Top sessions** and **Live requests** are `.usage-rows` / `.usage-feed`: the name or time first, then tokens, share and cost at the end. A failed request has the danger rule and its error.
- **Task and session totals** (`.usage-total`): one quiet faint line, *11K tokens · $0.42 estimated*, in the task's toolbar after its summary and in the session head after agent · age. Hovering shows the tokens by kind and the cost's caveat. The line grows live, follows the selected session, and hides from a tight session column (< 34rem). A page with no usage reader has no line.

### Code views
Reading code: a session's Code tab (Changes, Files, Map) and a repository's code page (Files, Map). The UX is in the task document *Design: code views — UX*, and every block and state is in the specimen's *Code views* section. Code is mono 13px on a fixed 20px line (`--size-code-line`) in the `code` well. Long lines scroll sideways inside the code, never the page, and line numbers stay put.
- **Code layout** (`.code-layout`): a side list (`aside.side`: the tree or the changed files, `clamp(14rem, 32%, 18rem)`) beside the reading surface (`section.main`). Each scrolls on its own. It is a size container named `code`, so it follows the room it gets: below a 44rem container one side shows at a time, the main side with `data-main`, with a `[data-code-back]` control back. `data-side="none"` has no side (the map).
- **Source bar** (`.code-source-bar`): the views switch, then which code is on screen (`.origin`: repository or a worktree select, `.branch` or the ref select, `.commit`, `.dirty`), its state and actions. `data-state="live"` is a teal dot and *live*. `stale` is an amber dot, *Changed on disk* and *Reload R*: an open file never moves under its reader. `gone` is a hollow dot and a `.gone` line saying what is shown instead, with its code and the way on. No `data-state` means a fixed ref.
- **Segmented** (`.segmented`): 2–5 sibling views or scopes, each its own URL (`a[aria-current]`) or a pressed button (`aria-pressed`), in a hairline box. The current one is raised. `aria-disabled` items carry a title saying why.
- **File tree** (`.file-tree`): a lazy ARIA tree. 28px rows (44px on touch) of chevron · icon · name · zone dot · change mark, indented by `--depth`. The open file is `aria-selected` (selected wash, signal icon). A loading directory holds skeleton rows. A deleted file stays listed, struck through. A symlink shows its target, and a submodule says so.
- **Changed files** (`.diff-summary`): totals and the scope switch (All · Branch · Working) over one row per file: change mark, the path truncated from its start so the name stays, counts or a faint word (*binary*, *not loaded*). The file on screen is `aria-current="location"`.
- **Change mark** (`.change-mark`): the git state as its letter in a fixed slot, coloured only to repeat it. Untracked is a dashed box, and a directory with changes inside is a dot.
- **Code lines** (`.code-lines`): the lines shared by files and diffs. The gutter is sized from `--digits`. Numbers come from `data-line` and the diff signs from `data-kind`, so a copied selection holds only code. Fixed-height lines can be virtualised. `data-wrap` and split rows grow, so they cannot. The targeted line takes `target-wash` and a bold number. A last line without a newline says so.
- **File** (`.code-view`): a head (the path as `.crumbs`, zone tags, language · lines · size, actions), an optional notice (truncated: what is shown and the path to read the rest elsewhere), then the lines, or a placeholder (binary, empty, symlink, submodule) with a sentence and no fake action.
- **Diff** (`.diff-view`): files with sticky heads (toggle, mark, path with `old → new` for a rename, zone tags, counts, actions) over their hunks. It is unified by default, and split from a 44rem container. A withheld file (`.withheld[data-reason]`) says why, with *Load diff* or the git command to run instead.
- **Crumbs** (`.crumbs`): a path as links, mono. The directories shrink and the file name never does.
- **Zone tag** (`.zone-tag`): a swatch in the context's hue (`data-hue`) and the name. `data-hue="none"` is dashed (a zone in no context), `data-kind="context"` bolds the name, and `data-dot-only` is the swatch alone for the tree.
- **Map** (`.code-map`): an overview bar of each context's share (no context dashed, unzoned hatched) over one card per context with its zones, a bar and a count. The count is always written. A project without zones shows the empty state.
- **Syntax** (`syntax.css`): the contract's 17 `tok-*` classes, scoped to `.code-lines`. Nothing else from a highlighter reaches the stylesheet.

### Transcript (signature)
The `sunken` well where a session's output is read: mono 13px, a grid of `time | mark | text`. Line kinds via `data-kind`: `user` (signal mark ›), `agent` (·), `tool` (muted, ⌁), `error` (red, ×), `prompt` (amber wash, ?) for a permission request awaiting the developer.

### Command palette, toast, empty state, skeleton
- **Palette:** a native `<dialog>` 36rem wide, 12vh from the top, `raised` with `line-strong` border and Float shadow, opening in 240ms ease-out-expo. The input is 48px and 16px type; results are 32px items with right-aligned kbd or meta.
- **Toast:** bottom-right region, `raised`, Float shadow; a leading status dot centers on the first line.
- **Banner:** inline notice; full 1px border tinted from its tone, role wash fill, leading icon, and a mono `.code` line carrying the error `code` and next action.
- **Empty state:** left-aligned in a 30rem column: 22px headline, one sentence that teaches what the thing is, then the primary action and a ghost alternative.
- **Skeleton:** 0.75em bars with a slow linear sweep; static under reduced motion. Used in place of spinners inside content.

### Magic pen and writing helper
- **Pen** (`.pen` in a `.pen-field`): an icon button inside a text field's end edge, always visible. A hairline and `raised` fill at rest, `signal` when the field has focus or the panel is open, dashed and faint when disabled ("Writing helper unavailable"). On a textarea it sits bottom-right clear of the resize grip; on a one-line input it is centred. The sparkle twinkles while the helper thinks; still under reduced motion. The field takes the `signal` edge while its panel is open and rings once when the helper writes it.
- **Helper panel** (`.helper`): non-modal, in flow under the field at the field's width (above it with `data-above`, for a field at the bottom of its pane), `raised` with `line-strong` border, a 2px `signal` top edge and Float shadow. Header (pen, title, "Reads …" chips, close), a scrolling `.body`, a `panel` footer with a hint and the actions. Under 40rem it is a bottom sheet (`data-sheet`) over a `.helper-scrim`.
- **Inside the panel:** numbered `.ask` questions (why line, input, `.chips`), `.draft` text marked with `ins` (underlined, signal wash) and `del` (struck through), `.gap` for an open placeholder (attention, dashed), `.seg` Changes/Clean, `.options` for title picks. The helper never writes the field: only Use this does, with a toast and Undo.

## 6. Do's and Don'ts

### Do:
- **Do** compose layout from the Every Layout primitives first, tune them with the `flow-space-*` / `gutter-*` utilities or their custom properties (`--sidebar-target`, `--switcher-threshold`, `--grid-min`), and keep blocks under 80–100 lines.
- **Do** express variants and state through `data-*` and ARIA attributes (`data-variant`, `data-state`, `aria-selected`, `aria-current`, `aria-invalid`, `aria-busy`) written inside the block's own file, never through modifier classes.
- **Do** read every color from a semantic role (`--color-ink-muted`, `--color-signal`, `--color-panel`); a raw `oklch()` outside `tokens.json` is a bug.
- **Do** pair every status color with its word, and announce meaningful session state changes through the stream bar's polite live region.
- **Do** show the error `code` and the next action whenever the API returns a `StructuredError`, in a `.banner` with its `.code` line.
- **Do** keep transitions between 90 and 240ms with `--ease-out`, and give every animation a reduced-motion fallback.
- **Do** keep text contrast ≥ 4.5:1 and control edges ≥ 3:1 in both themes; re-check when adding a surface.
- **Do** pair every code state with its letter, sign or word, and keep code lines a fixed `--size-code-line` high unless they wrap.
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
- **Don't** use syntax or context colours outside the code blocks, or size code lines and gutters from their content (no per-line content-sized tracks).
- **Don't** restyle native scrollbars or checkboxes beyond theme color (`accent-color`, `scrollbar-color`). Selects follow the Select variant in §5 Dropdowns: the closed control matches the inputs; the open list stays the browser's, or base-select where supported.
