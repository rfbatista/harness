---
name: deslope
description: Use when a branch's diff needs a cleanup pass for AI-generated code slop before review or merge — inconsistent comments, unneeded defensive checks, or `any` casts introduced during the work.
---

# Deslope

Check the diff against main, and remove all AI-generated slop introduced in this branch.

## What counts as slop

- Extra comments that a human wouldn't add, or that are inconsistent with the rest of the file
- Extra defensive checks or try/catch blocks that are abnormal for that area of the codebase (especially in code called only by trusted / already-validated codepaths)
- Casts to `any` (or equivalent) used to get around type issues instead of fixing them
- Any other style that is inconsistent with the surrounding file

## What to do

1. Diff the current branch against `main` (or the repo's default branch).
2. Walk each changed file and remove slop matching the categories above — don't touch unrelated code.
3. When unsure whether something is slop or intentional, leave it; this is a cleanup pass, not a rewrite.
4. Report at the end with only a 1-3 sentence summary of what was changed.
