---
name: maintain-verification-skill
description: Use in a harness architect-mode session only when the person asked to audit a repository's verification skill, or agreed to your offer through ask_person — read-only sub-agents check each feature file of features-<app> against the source, an Operator and a QA drive every feature live, and the proven corrections go to the person as one update instruction-file proposal. Never run it unasked.
---

# Maintain a verification skill

Run this skill only when the person invoked it or agreed to your offer through `ask_person`. Never run it unasked.

A feature map rots the moment the application changes. This is the upkeep loop for the
two skills `create-verification-skill` writes: `verify-<app>` (launch, doctor, drive,
evidence, cleanup) and `features-<app>` (the feature map). The unit of rigor is the
feature, not every sentence: cover every feature file from the source, and exercise every
feature live, without proving every bullet.

Adapted from `maintain-verification-skill` in https://github.com/cursor/plugins
(`pstack/skills/maintain-verification-skill`, commit
`bf0c7cd80abdbf7fe47b634978a7d1b4071f71a7`, MIT, © 2026 Lauren Tan). The licence is
`LICENSE` in this folder.

## Before you start

If you are not in architect mode, run no step: tell the architect what is missing.

You never run the application, never edit product code and never write into the
repository. You read the source, spawn the sessions that run the application, and edit
the draft. The person approves the result. Ask the person through `ask_person`, with
your recommended answer, and end your turn there; the answer arrives as your next turn.
Never ask in plain text.

Read large files in parts: `grep -n` for the heading, then `sed -n` that range, never the
whole file. Change a file with Edit, not by rewriting it. A write refused with
`BASE_VERSION_STALE` means the file changed: read it again and redo the change.

## Outcomes

Pick one, and say which:

- **clean:** every feature got source and live coverage; nothing is worth shipping. No
  proposal.
- **changed:** one update proposal ships proven doc, harness or map corrections.
- **blocked:** coverage could not finish, or a proven fix could not ship safely. Say
  exactly what blocked it.

## Edit scope

Edit only the two skills' own files, in the draft. Copy the current files from
`project/architecture/<repository>/instructions/` into
`task/instruction-drafts/<repository>/<skill>/` first, then change the copies. Never edit
product code during a run. A behaviour the map describes that the application no longer
has is either **doc drift** (fix the map) or a **product regression** (report it, never
paper over it in the docs).

## Pass

Run the steps in this order.

0. **Locate the target.** Find `verify-<app>` and `features-<app>` in
   `project/architecture/<repository>/instructions/`. Several applications: ask which one
   through `ask_person`. None: stop and point at `create-verification-skill`. Never
   invent a target.
1. **Index hygiene.** Read the `features-<app>` `README.md` and list its sibling files.
   Fix missing, extra, duplicate or dead entries. Keep it light: no generated inventory.
2. **Source wave.** One read-only sub-agent (the Agent tool) per feature file, launched
   together. Each explains how that user-facing feature works from the source, flags
   likely doc drift with citations, and returns one concise live recipe. A sub-agent
   never drives the application and never edits a file. It returns: feature summary,
   source entry points, likely drift or none, one recipe.
3. **Reconcile.** Every feature file has a returned summary. Merge overlapping recipes
   into as few application states as practical. Spot-check cited drift; do not re-prove
   clean claims. Sweep recent churn for user-facing surfaces missing from the map, and
   call one missing only with a concrete source path.
4. **Live pass.** Required even when the source looks clean. Spawn an **Operator**
   (`mcp__task__start_task_session`, `mode: "operate"`, default branch as `base_branch`)
   to follow `verify-<app>`'s Launch and Doctor sections, and a **QA** (`mode: "qa"`)
   once the Operator reports the application running with its URLs. You own the plan;
   they own all driving. Follow the skill's own launch model: one long-lived instance
   driven serially for servers and UIs, or a fresh isolated session per drive for
   short-lived CLIs. The QA exercises every feature at least once, and three things hold
   the whole pass, whatever fails:
   1. Never drive an instance that was not health-checked since it last did something
      surprising. Doctor before the first drive, after any failed drive, and on each
      fresh session. Where doctor cannot see the failure (a wedged UI on a healthy
      process), reset to a known state or relaunch.
   2. Evidence captured so far survives every cleanup, checked at its named place in
      `task/designs/`, not assumed.
   3. Nothing a drive started outlives its usefulness. Residue of a failed drive is
      cleaned whether the session is stuck, exited or shared. For a shared instance,
      clean the residue, not the instance.

   A doctor failure caused by skill drift is drift: fix the draft and retry once,
   restarting only what the fix invalidated, before you call the pass `blocked`. A
   feature that cannot be reached is `verified-unreachable` only with the concrete
   prerequisite (auth, entitlement, OS, external state) and the route attempted; if the
   map omits that prerequisite, that is drift. Re-drive every harness fix from triage
   live before it ships. Final teardown comes after the last drive, re-proofs included.
   Evidence stays.
5. **Triage.** Wrong or missing user-POV description: doc drift, fix it. Working
   behaviour the harness cannot drive: harness gap, fix it, under the same helpers rule
   as generation (a script is shown invoked through its interpreter from the skill's
   folder, for example `bash <skill folder>/scripts/doctor.sh`, never relying on an
   executable bit; take `<skill folder>` from the "Base directory for this skill" line
   the loader prints). Application behaviour that is actually broken: product gap;
   record it for the person and keep it out of the proposal.
6. **Ship or stop.** For **changed**, re-read every changed draft file, then submit one
   update proposal:

   ```
   propose_instruction_files { repository, skills: ["verify-<app>", "features-<app>"], summary }
   ```

   Name in the summary what drifted, what you re-proved live, and the product gaps you
   left out. A send-back note arrives as a turn: carry it into the draft, re-prove what
   it touches, and submit again. For **clean** or **blocked**, submit nothing; report the
   outcome and the coverage honestly.

Keep short run notes (features covered, unreachable prerequisites, confirmed drift,
outcome) in a task document under `task/docs/`, never in the draft.

## Refusals

| Situation | Do |
|---|---|
| The person did not ask and did not agree | Do not start. Offer through `ask_person` once. |
| No `verify-<app>` exists | Stop. Point at `create-verification-skill`. |
| You are about to run the application | Stop. Spawn an Operator. |
| A correction was not re-driven live | Leave it out of the proposal. |
| Product code looks wrong | Report it as a product gap. Never edit it. |
