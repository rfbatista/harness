# Product

## Register

product

## Users

One developer: the person who built the harness and runs it on their own machine. They switch between several projects, each with its own agents, skills, MCP servers, tickets and live Claude sessions. They open the UI in a browser next to their editor and terminal, usually while one or more agents are already working. Most of the time they are glancing: what is running, what finished, what is stuck or waiting on them. Some of the time they are working: configuring an agent, writing a ticket, reading a session transcript to see what an agent actually did.

## Product Purpose

The harness (coding_pool) is an AI-architecture orchestrator: a stateful server that holds the catalog (agents, skills, MCP servers), the work (projects, zones, tickets, tasks) and the live agent sessions that act on it. The new web UI is a browser client of the existing HTTP API (`/api`) and its SSE event streams, served by the Go server. It replaces the stale Architecture Designer.

Success means the developer can leave a handful of agents running and know their state at a glance without hunting for it. They can step into any session, ticket or config in one or two keystrokes, and they trust that what the screen shows is what the server holds.

## Brand Personality

Calm, exact, developer-native. A control room, not a cockpit: many live things on screen, but the surface stays quiet until something actually needs attention. The tone is terse and factual, like good CLI output: it names the state, the cause and the next action, without cheerleading. The emotional goal is quiet confidence. Nothing is blinking for its own sake, and when something does change it is worth looking at.

References:
- **Warp / Zed**: developer-native, terminal-adjacent; crisp monospace and sans type working together; fast and keyboard-driven; chrome that feels engineered, not decorated.
- **Vercel dashboard**: spare, near-monochrome; status carried by small, precise signals (a dot, a word, a timestamp) on lists of deployments rather than by big panels.

## Anti-references

- **Busy IDE chrome**: dozens of panels, toolbars, icon rows and tabs competing for attention. Panes appear only when the task needs them; a screen has one primary job.
- By extension, the current designer's Tailwind/daisyUI defaults (stock purple primary, pink secondary) are not the identity to keep.

## Design Principles

1. **Quiet until it matters.** Steady state is calm and low-contrast in color; attention (color, motion, position) is spent only on state changes the developer must act on: a failed session, a session waiting for input, a blocked ticket.
2. **Live means trustworthy.** The UI reflects server state as it streams in over SSE. Staleness, reconnection and errors are shown, never hidden; a coded error is presented with its code and next action.
3. **Keyboard first, mouse welcome.** Every action can be reached from the keyboard and discovered (command palette, visible shortcuts), mirroring the tui-client's keys where they overlap.
4. **One job per screen.** Show a pane when it serves the current task, not just because there is space for it. Density comes from good lists and typography, not from more panels.
5. **Speak the domain.** Use the vocabulary of `docs/CONCEPTS.md` (projects, zones, agents, skills, tickets, sessions) exactly; labels and copy read like the API and the CLI.

## Accessibility & Inclusion

- WCAG 2.2 AA: text contrast ≥ 4.5:1 (≥ 3:1 for large text and UI component boundaries), including muted and placeholder text.
- Full keyboard operability with visible focus everywhere; no keyboard traps in live terminal/session views (a documented escape key).
- Status never conveyed by color alone: always paired with a word, icon or shape (important for running/failed/waiting states).
- `prefers-reduced-motion` respected: live updates fall back to instant changes, without pulsing or animated transitions.
- Live regions announce meaningful session state changes politely, without narrating every streamed line.
