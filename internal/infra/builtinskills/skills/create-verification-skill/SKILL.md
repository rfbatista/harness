---
name: create-verification-skill
description: Use in a harness architect-mode session only when the person asked for a verification skill for a repository, or agreed to your offer through ask_person — write verify-<app> (launch, doctor, drive, evidence, cleanup) and features-<app> (the feature map) as an instruction draft, have an Operator and a QA prove it against the running application, then submit both as one instruction-file proposal. Never run it unasked.
---

# Create a verification skill

Run this skill only when the person invoked it or agreed to your offer through `ask_person`. Never run it unasked.

Every serious repository needs a scripted way to drive its real application and prove
behaviour: launch it, exercise a feature the way a user would, capture evidence. You
write that as two **instruction-file skills** for the repository: `verify-<app>` and
`features-<app>`. You write them for the next agent, who reads them cold, mid-task,
and has never seen the application.

Adapted from `create-verification-skill` in https://github.com/cursor/plugins
(`pstack/skills/create-verification-skill`, commit
`bf0c7cd80abdbf7fe47b634978a7d1b4071f71a7`, MIT, © 2026 Lauren Tan). The licence is
`LICENSE` in this folder.

## Before you start

Run this skill only when the person invoked it, or said yes to your offer through
`ask_person`. Never run it unasked and never offer it twice after a no. If you are not
in architect mode, run no step: tell the architect what is missing.

You never run the application, and you never write into the repository. You write the
draft, an Operator launches the application, a QA drives it, and the person approves the
proposal. Ask the person through `ask_person`, with your recommended answer, and end
your turn there; the answer arrives as your next turn. Never ask in plain text.

Reference files, in this folder, read only when a step names them:

- `references/feature-map-example/` — the shape of a feature map: `README.md` (the
  index) and two feature files.
- `LICENSE` — the upstream MIT notice.

Read large files in parts: `grep -n` for the heading, then `sed -n` that range, never
the whole file. Change a file with Edit, not by rewriting it. A write refused with
`BASE_VERSION_STALE` means the file changed: read it again and redo the change.

## 1. Interview the repository, not the person

Answer these from the code. Ask the person only what you cannot observe.

- **Surface:** what does a user touch? A web UI, a CLI or TUI, a desktop app, an API, a
  library? Pick the primary one and note the rest.
- **Run:** how does the application start locally? Prefer the repository's own dev
  command (package scripts, Makefile, README quickstart). Note ports, variables, seed
  data, auth.
- **Drive:** how can an agent interact with it? Existing harnesses first (Playwright or
  Cypress specs, expect scripts, curl-able endpoints, a debug port). Then a generic
  recipe: the browser MCP tools for web UIs, a terminal harness for CLIs, plain HTTP
  for services.
- **Observe:** what evidence can be captured? Screenshots, terminal transcripts,
  response bodies, logs, exit codes, stored state.
- **Isolate:** can two instances run side by side (ports, data directories, profiles)?
  If not, say so in the skill: refusing to double-drive a shared instance beats
  corrupting someone's session.

If the checkout does not build or start as it is, report it precisely before you write
anything: a skill written against a broken base teaches wrong steps. An irrelevant
missing asset that blocks startup may be created by the skill, marked as verification
scaffolding, and removed in cleanup.

## 2. Write the draft

The draft is the skill as it would be saved. Write it in the task's instruction draft
folder, never in the repository:

- `task/instruction-drafts/<repository>/verify-<app>/SKILL.md` (and `scripts/` if it
  ships helpers)
- `task/instruction-drafts/<repository>/features-<app>/SKILL.md`, `README.md` and one
  `.md` per feature

`<repository>` is this repository's name; `<app>` is the application's short name. A
refused write names its code: read it and fix the path.

`verify-<app>/SKILL.md` has YAML frontmatter (`name: verify-<app>` and a `description`
that names the application, the surface and when to reach for it; without frontmatter
the skill never registers) and these sections. Ground each in what the interview found,
with no placeholder left:

- **Launch.** The exact commands that start the application for verification, and how
  to tell it is ready (a log line, a prompt, a health URL). Ready means usable, not
  only that a process answers: migrations and seed data are done. Launch must reach a
  usable state from an empty data directory. In a harness session an **Operator**
  (an operate-mode session) launches it through service requests on the task's branch;
  the skill names the service, its command, its variables and its ready check, and
  says the Operator's done report gives the URLs. A short-lived CLI has no server:
  launch means build the binary once, then start each drive in its own isolated
  terminal session.
- **Doctor.** One read-only check that answers "is this instance worth driving?":
  process up, right build, port owned by this run, auth valid. A session runs it first
  whenever anything looks off.
- **Drive.** The recipe, with real selectors and commands from this repository. For a
  web UI use the browser MCP tools. Chrome runs with `--no-sandbox` and its
  `--user-data-dir` under `$TMPDIR`, because the agent sandbox cannot nest Chrome's own.
  Prefer stable handles (ARIA roles and names, data attributes, prompt strings, routes)
  over coordinates and tab order.
- **Evidence.** What to capture and where it goes: screenshots and videos in
  `task/designs/`, transcripts and notes in a task document. State the proof
  standards: exercise the real user path, never internal setters or test-only
  endpoints; capture the action and the resulting state, not only the final screen;
  verify side effects (files, rows, messages) beside what is visible; mock only where a
  production boundary already isolates the external system. When the safe path is a
  dry run, observe what it skips (files, network, git refs) rather than trusting its
  name.
- **Cleanup.** How to tear down what the run created. Stop only what the run started,
  by the service or process id the run recorded. Never kill by process name, never
  `pkill`, never stop another session's service. Cleanup removes instances and scratch
  state, never the evidence: artifacts survive teardown, in the place the skill names.
- **Helpers.** Any script the skill ships is shown invoked through its interpreter from
  the skill's own folder, for example `bash <skill folder>/scripts/doctor.sh` or
  `python3 <skill folder>/scripts/seed.py`. Instruction files store text only, so a
  script never has an executable bit: never write `./scripts/x.sh`, never rely on
  `chmod`. A delivered skill's folder is the one its loader names ("Base directory for
  this skill: …") when the skill loads, a temporary directory per session. Tell the
  reader to take `<skill folder>` from that line and never to hard-code a path. A helper
  the reader has to reverse-engineer is not a helper.

## 3. Write the feature map as its own skill

`features-<app>` is a separate skill, so any session can load it to learn what the
application does from the user's side. It has three kinds of file, in the shape of
`references/feature-map-example/`:

- `SKILL.md`: short. Frontmatter `name: features-<app>` and a `description` that says
  when to load the map (to learn or verify what <app> does for its user), then one line
  pointing to `README.md`. Nothing else.
- `README.md`: the index, as upstream's example. Baseline preconditions, driving
  conventions, proof and skip reporting, the feature entry contract, the feature list
  with links.
- One `.md` per user-facing feature (aim for the top three to five to start, from
  routes, commands, menus or docs). Each has an H1, one paragraph, and exactly four H2
  sections in this order: `Sub-features`, `How to get to it (user POV)`,
  `Driving it with <harness>`, `Gotchas`.

If the seed data lacks a precondition a journey needs, the journey reports a failure and
the reason goes in that feature's `Gotchas`. Never weaken a journey to make it pass.
`verify-<app>` points to `features-<app>` for its drive recipes and does not repeat
them. A proof that drives one convenient entry point is incomplete when the map lists
others.

## 4. Prove the draft before you propose it

A draft that was never run is a draft, not a deliverable. You prove it with two sessions
of your own, never by running the application yourself:

1. Spawn an **Operator** with `mcp__task__start_task_session`, `mode: "operate"` and
   the default branch as `base_branch`. Its prompt names the draft's Launch and Doctor
   sections by path and asks it to launch the application through service requests, run
   the doctor and report what failed, with the URLs.
2. Wait for its done report. Fix the draft from what it reports and spawn a new
   Operator if the launch changed. Answer its questions with `reply_to_session`.
3. Spawn a **QA** with `mode: "qa"` once the Operator reports the application running.
   Its prompt names `verify-<app>`, one mapped feature file, and the URLs. It drives
   that one feature, publishes the evidence in `task/designs/` and reports.
4. Fix the draft from the QA's report, including every step the QA had to guess.
   Confirm from the report that the evidence still exists after cleanup. Repeat from
   step 3 until one feature passes by the draft alone.

If you proved the draft in a stand-in for the real target (a local stack in place of a
remote one), list in the proposal's summary every setup step the stand-in did that the
target will not. Those steps stay unverified.

## 5. Propose it

Submit both skills in one call, so the person approves them together:

```
propose_instruction_files { repository, skills: ["verify-<app>", "features-<app>"], summary }
```

The summary says what the application is, what you proved, with which feature, and what
stays unverified. A skill that already exists is updated; a new one is created. The
person approves, rejects, or sends the proposal back with a note. A send-back note
arrives as a turn: carry it into the draft files, prove again whatever it touches, and
submit the same call. Do not save instruction files any other way and never commit them
to the repository.

## 6. Offer the maintenance loop

When the proposal is approved, tell the person that `maintain-verification-skill` keeps
the map honest as the application changes. Suggest a cadence only if they ask.

## Refusals

| Situation | Do |
|---|---|
| The person did not ask and did not agree | Do not start. Offer through `ask_person` once, with your recommendation. |
| The checkout does not build or start | Report it with the failing command. Write no draft. |
| You are about to run the application | Stop. Spawn an Operator. |
| A helper would need an executable bit | Show it run through its interpreter instead. |
| The draft was not proven | Do not call `propose_instruction_files`. |
