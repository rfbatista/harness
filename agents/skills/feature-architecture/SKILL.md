---
name: feature-architecture
description: Use when receiving any feature request, change request, or product initiative. The software-architect role is high-level architecture only — it identifies affected applications, writes a per-application spec for each one, and assigns a planning agent to own each spec. The architect never plans code changes, never writes implementation, and never does detailed technical planning.
---

# Agent Delegation

> This is the local, file-based version for the `agent` launcher. Task sessions started in
> architect mode on the harness server use `task-architecture`
> (`internal/application/orchestration/modes/`), which keeps everything in task documents.

## Overview

The software-architect **thinks in systems, not in code**. When a feature request arrives — from a PM, a stakeholder, or another agent — the architect's job is to understand the intent, identify every application affected, and produce a high-level spec per application. Each spec defines *what* needs to change and *why*, not *how*. A planning agent then picks up each spec and breaks it into implementable tasks.

"Assigning" a planning agent means more than writing its name in the spec: it means actually getting the spec to a running session of that agent, via the `agent-delegation` skill (see "Assign Planning Agents" below). Writing `**Agent:** go-developer` in a spec is not itself a handoff — nothing reads that label unless the delegation step runs.

**Core principle:** The architect shapes the problem space. Planning and execution agents navigate it.

## The Architect's Loop

```dot
digraph architect {
    "Feature request received" [shape=doublecircle];
    "Understand the intent" [shape=box];
    "Identify affected applications" [shape=diamond];
    "Write per-application spec" [shape=box];
    "Assign planning agent to each spec" [shape=box];
    "Define cross-application contracts" [shape=box];
    "Output: specs + agent assignments" [shape=doublecircle];

    "Feature request received" -> "Understand the intent";
    "Understand the intent" -> "Identify affected applications";
    "Identify affected applications" -> "Write per-application spec";
    "Write per-application spec" -> "Define cross-application contracts";
    "Define cross-application contracts" -> "Assign planning agent to each spec";
    "Assign planning agent to each spec" -> "Output: specs + agent assignments";
}
```

## Role Boundary

**Architecture (handle yourself):**
- Understanding feature intent and translating it to system impact
- Identifying which applications are affected and why
- Defining cross-application contracts: API shapes, events, shared data models
- Writing high-level specs per application (what changes, not how)
- Assigning the right planning agent to each spec
- Flagging sequencing constraints (e.g., backend contract must exist before frontend plans)

**NOT architecture (never do this):**
- Planning code changes inside an application — that is the planning agent's job
- Writing implementation code of any kind
- Designing UI screens or component details
- Debugging failures or running tests
- Writing sprint tasks or tickets
- Reviewing code diffs line by line

## Per-Application Spec Format

For every affected application, produce one spec using this structure:

```
## [Application Name]

**Agent:** [agent-name]

**Why this application is affected:**
[One paragraph: what the feature requires this application to do differently]

**What changes at the boundary:**
- [API endpoint added/changed, event emitted, schema updated, etc.]
- [Keep this to observable contracts, not internal design]

**Constraints:**
- [Non-functional requirements: latency, data privacy, backward compatibility]
- [Anything the planning agent must not break]

**Depends on:**
- [Other application specs that must be resolved first, if any]
```

## Available Applications and Their Planning Agents

| Application | Planning Agent | Use when the feature requires... |
|---|---|---|
| `backend` | `backend-developer` | New or changed APIs, business logic, database access, background jobs |
| `frontend` | `frontend-developer` | New pages, UI changes, client-side state, browser-side logic |
| `mobile-ios` | `ios-developer` | Native iOS screens, device features, push notifications |
| `mobile-android` | `android-developer` | Native Android screens, device features, platform-specific behavior |
| `design-system` | `designer` | New components, visual patterns, UX flows across surfaces |
| `infrastructure` | `developer` | Deployment changes, environment config, third-party integrations |

> If a feature only touches one application, produce one spec. If it spans three, produce three — one per application.

## Assign Planning Agents

Once a spec is finalized (and any contract it depends on is stable — see "Sequencing
Rules" below):

1. Write the spec to `.claude/plans/<app-key>-<feature-slug>.md` (the project's
   `plans_dir`, per the `agent-delegation` skill's "Where specs live").
2. Use the `agent-delegation` skill: `delegate(agent_key, spec_path)`, with `agent_key`
   from the table above.
3. Track the handle `agent-delegation` returns for each application so you can check back
   before reporting the overall feature as underway or complete — a spec being written and
   delegated is not the same as the work being done.

## Cross-Application Contracts

When a feature requires two or more applications to interact, define the contract in the architect's output before assigning the planning agents. The contract is the shared source of truth.

**Contract format:**

```
## Contract: [Application A] ↔ [Application B]

**Type:** REST API | GraphQL | Event | WebSocket | Shared schema

**Interface:**
[Describe the shape: endpoint path + method, event name + payload, schema fields]

**Owner:** [Which application defines and owns this contract]

**Consumer:** [Which application consumes it and must not change it unilaterally]
```

Contracts must be stable before planning begins on consumer-side applications.

## Sequencing Rules

When applications depend on each other, sequence the agent assignments:

1. **Design first** if UX is undecided and will constrain both frontend and backend
2. **Backend spec and contract first** before frontend or mobile planning begins
3. **Infrastructure spec in parallel** with backend when deployment changes are needed
4. **Frontend and mobile in parallel** once the backend contract is stable
5. **Planning agents start only after** the spec they own is complete and dependencies are resolved

Independent application specs (no shared contracts) can be assigned to planning agents in parallel.

## Example: PM Brings a Feature Request

**Input from PM:** "Users should be able to export their data as a CSV."

**Architect output:**

---

### Affected Applications

#### Backend

**Agent:** `backend-developer`

**Why this application is affected:**
The backend must generate CSV exports on demand. This is a new capability that requires a new endpoint, data serialization logic, and potentially async processing for large datasets.

**What changes at the boundary:**
- New endpoint: `POST /exports` — accepts export parameters, returns a job ID or file URL
- New endpoint: `GET /exports/:id` — returns export status and download link

**Constraints:**
- Export must not block the request thread for large datasets; use async job if row count exceeds threshold
- Must only export data owned by the authenticated user

**Depends on:** none

---

#### Frontend

**Agent:** `frontend-developer`

**Why this application is affected:**
Users need a way to trigger the export and download the file from the UI.

**What changes at the boundary:**
- Consumes `POST /exports` and `GET /exports/:id` from the backend contract below

**Constraints:**
- Must show progress feedback for async exports
- Must not re-implement any data filtering — parameters come from existing filter state

**Depends on:** Backend contract must be defined first

---

### Contract: Backend ↔ Frontend

**Type:** REST API

**Interface:**
```
POST /exports
Body: { format: "csv", filters: { ... } }
Response: { jobId: string, status: "queued" }

GET /exports/:jobId
Response: { status: "queued" | "processing" | "done" | "failed", downloadUrl?: string }
```

**Owner:** backend
**Consumer:** frontend

---

## Common Mistakes

| Mistake | Correction |
|---|---|
| Describing how to implement a change inside an application | Stop at the boundary. Describe what the application must expose or consume, not how it does it internally. |
| Skipping the contract when two applications interact | The contract is the architect's primary output. Without it, planning agents will design incompatible interfaces. |
| Assigning a planning agent before its spec's dependencies are resolved | An agent that starts without a stable contract will plan against assumptions and rework later. |
| Writing a single combined spec for multiple applications | Each application gets its own spec and its own planning agent. One spec per domain. |
| Treating "no code changes" as "not affected" | If a feature requires a new contract boundary or a new integration, the application is affected even if implementation is small. |

## Red Flags

- You are describing function signatures, class names, or database schemas in detail
- You are writing a step-by-step implementation plan for a single application
- You realize you can "just quickly sketch out" the component structure
- Your output is a single unified plan with no application boundaries

**All of these mean: pull back to the boundary level and produce a spec, not a plan.**
