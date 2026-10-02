---
name: software-engineering-best-practices
description: Use when making architectural decisions, designing new systems, reviewing code structure, choosing between design patterns, evaluating trade-offs in scalability or consistency, or when guidance on foundational principles for building maintainable and scalable software is needed.
---

# Software Engineering Best Practices

## Overview

Every architectural decision involves a trade-off. The goal is to maximize cohesion, minimize coupling, and design systems that are easy to change — not systems that never change.

## 1. Core Architecture Principles

### SOLID
| Principle | Rule |
|---|---|
| **S**ingle Responsibility | One reason to change per module/class/function |
| **O**pen/Closed | Open for extension, closed for modification |
| **L**iskov Substitution | Subtypes must be substitutable for base types |
| **I**nterface Segregation | Many small interfaces over one "God Interface" |
| **D**ependency Inversion | Depend on abstractions, not concretions |

### Cohesion & Coupling
- **High cohesion**: every part of a module serves a single, well-defined task (Functional Cohesion)
- **Low coupling**: components don't rely on each other's internal implementation
- **Connascence**: prefer static (compile-time) over dynamic (runtime) dependencies

### Layered Architecture
Typical layers: `Presentation → Application → Domain → Infrastructure`
- Each layer depends only on the layer directly below
- Domain layer contains pure business rules — no UI, no DB code

### Domain-Driven Design (DDD) Essentials
- **Ubiquitous Language**: same words in code, diagrams, and conversations with domain experts
- **Bounded Context**: define explicit boundaries where a model applies
- **No Anemic Domain Models**: objects should encapsulate both state *and* behavior

## 2. Simplicity Principles

| Principle | Rule |
|---|---|
| **DRY** | Every piece of knowledge has one authoritative representation |
| **KISS** | Eliminate complexity; avoid "nice-to-have" features |
| **YAGNI** | Only write code that is actually needed now |
| **Encapsulation** | Hide internals; expose intention-revealing interfaces only |

## 3. System Design Patterns & Trade-offs

### Scaling
- **Vertical** (scale up): more powerful single machine — simpler, hits a ceiling
- **Horizontal** (scale out): more machines — cost-effective, requires concurrency; database is usually the bottleneck

### Caching
| Strategy | Pros | Cons |
|---|---|---|
| Replicated (in-memory per node) | Fastest, no single point of failure | Collision risk during replication lag |
| Distributed (central cache) | High consistency | Network latency, single point of failure |

Use replicated for static data; distributed for dynamic data (e.g., inventory).

### Database Choices
- **SQL**: best for large interconnected data sets with complex queries; normalize to follow DRY
- **NoSQL**: preferred in microservices; match storage engine to bounded context needs
- **Denormalize** when access time > storage space (distributed local copies avoid remote calls)
- **Index** strategically to avoid expensive full scans

### API Design
- **REST**: resources + standard HTTP verbs; version early (major increment = breaking change)
- **Parsimony**: exchange exactly the right data with minimum interactions; expose the smallest needed API surface
- Rate limiting and versioning are non-optional for public APIs

### Distributed Systems
| Concept | Key Point |
|---|---|
| **CAP Theorem** | Can't have Consistency + Availability + Partition Tolerance simultaneously |
| **Eventual Consistency (BASE)** | Favor availability/scalability over immediate consistency |
| **EDA — Broker topology** | Decoupled, scalable; weak error handling, no central control |
| **EDA — Mediator topology** | Central orchestration; better error handling, potential bottleneck |

## 4. Code Quality

- **Naming**: use domain language; names describe *effect and purpose*, not implementation
- **Functions**: small (5–24 lines), single task, ≤4 parameters
- **Error handling**: distinguish technical exceptions (DB down) from business exceptions (insufficient funds); use Notification Pattern to collect multiple errors rather than throwing per field
- **Avoid null returns** for failed operations — use typed results or exceptions

## 5. Testing Strategy

**Testing Pyramid (bottom to top):**
1. **Unit** — isolated behavior; use Given-When-Then; mock dependencies
2. **Integration/Component** — verify units interact correctly through interfaces
3. **E2E/System** — find emergent behavior and unanticipated interactions

**TDD cycle**: Red → Green → Refactor. Target 70–90% coverage.

**Principles:**
- Test *required behavior*, not implementation details (tests that break on refactor = brittle tests)
- Use property-based testing for complex invariants

## 6. Observability

**Logging:**
- Categories: debugging, error recovery, performance tuning, behavior tracking
- Format: structured (JSON), always include timestamp + traceable ID
- Levels: DEBUG / INFO / WARN / ERROR — keep logs clean and actionable

**Metrics:**
- **Cyclomatic Complexity**: keep below 10 (ideally ≤5); high CC = error-prone component
- Track: error frequency, cycle times, resource utilization

## 7. Technical Debt & Refactoring

- Treat debt as a loan: short-term gain, long-term interest in maintenance cost
- Track explicitly: TODO / REDO / HACK tags or task cards
- **Entropy reduction**: schedule dedicated episodes every 9–12 months after major releases
- **Safe refactoring**: always have automated tests first; move incrementally
- **Focus**: refactor the Core Domain (highest business value) first, not generic subdomains

## Common Mistakes

| Mistake | Fix |
|---|---|
| God classes / modules | Apply SRP; split by bounded context |
| Premature abstraction | YAGNI; abstract only when the pattern repeats ≥3 times |
| Ignoring the CAP theorem | Explicitly choose your trade-off; document it |
| Tests that mirror implementation | Test behavior (what), not structure (how) |
| Technical debt without tracking | Add tags + schedule entropy reduction |
| Anemic domain models | Move behavior into domain objects |
