---
name: gof-design-patterns-typescript
description: Use when choosing or naming an object-oriented design in TypeScript - a growing switch/if-else chain on type, a constructor with many optional arguments, subclass explosion, an incompatible third-party interface, undo/redo or history, or code review asking "which pattern is this". Also use when unsure whether a classic GoF pattern is warranted at all.
---

# GoF Design Patterns in TypeScript

## Overview

The 23 GoF patterns are not a menu of good designs. Each one buys **one specific
axis of change** and pays for it with an extra indirection. Name the change
pressure first; pick the pattern that isolates exactly that pressure.

**Core principle:** a pattern is justified by a change that has already happened
twice, or by a boundary you do not control. Not by one that might happen.

Most GoF structure was designed for languages without first-class functions,
union types, or structural typing. TypeScript has all three, so several patterns
collapse to a few lines — each reference entry names the collapse under
**Skip it when**.

## When to Use This Skill

- A `switch`/`if-else` chain on a type tag keeps growing (→ Strategy, State, Factory Method, Visitor)
- Subclasses multiply along two independent axes (→ Bridge, Decorator)
- You must consume an interface you cannot change (→ Adapter, Facade, Proxy)
- The feature is undo/redo, history, queued or replayable actions (→ Command, Memento)
- Object construction has many optional or order-dependent steps (→ Builder)
- Parts of the system need to react to changes without knowing each other (→ Observer, Mediator)
- Review asks "what pattern is this" or "is this over-engineered"

## When NOT to Use a Pattern

- **One implementation exists and none is planned.** Write the direct code.
- **A function, closure, or union type does the job.** `type Shipping = 'air' | 'sea'` + a lookup object beats a Strategy hierarchy.
- **The variation point is data, not behavior.** Configure it; don't subclass it.
- **You are naming existing code.** Naming a class `FooFactory` changes nothing. Extract the seam or leave it.

## Selection Procedure

1. Write one sentence: *"`<what>` will change independently of `<what else>`."* If you cannot, stop — no pattern.
2. Classify the change: **what gets created** (creational), **how objects are wired** (structural), **how responsibility flows at runtime** (behavioral).
3. Pick the smallest pattern in that category from the table below.
4. Check the TypeScript collapse in the reference file before writing a class hierarchy.
5. Keep the client depending on the interface, never on the concrete class.

## Symptom → Pattern

| Symptom | Pattern | Category |
|---|---|---|
| Client picks a concrete class by `switch` on a tag | Factory Method | creational |
| Products must be created in matching families (theme, platform, vendor) | Abstract Factory | creational |
| Constructor takes 8 args, half optional; several build orders | Builder | creational |
| Copying an object is expensive or its class is unknown at runtime | Prototype | creational |
| Exactly one shared instance must coordinate the app | Singleton | creational |
| Third-party interface does not match yours | Adapter | structural |
| Subclasses multiply as `Kind × Platform` | Bridge | structural |
| Tree of items where leaf and group must be treated alike | Composite | structural |
| Behavior must be stacked/layered at runtime, in any order | Decorator | structural |
| A subsystem exposes 12 classes; callers need 2 calls | Facade | structural |
| Millions of objects, most fields duplicated | Flyweight | structural |
| Need lazy init, access control, caching, or logging on an object | Proxy | structural |
| Request should pass through ordered, optional handlers | Chain of Responsibility | behavioral |
| Actions must be undoable, queued, logged, or replayed | Command | behavioral |
| Traversal order varies, or collection internals must stay hidden | Iterator | behavioral |
| N components each talk to N others | Mediator | behavioral |
| Snapshot and restore state without exposing internals | Memento | behavioral |
| Many objects must react to one object's state change | Observer | behavioral |
| Object's behavior changes wholesale with its state; illegal transitions matter | State | behavioral |
| One task, several interchangeable algorithms | Strategy | behavioral |
| Fixed algorithm skeleton, varying steps | Template Method | behavioral |
| New operations added often over a stable set of node types | Visitor | behavioral |

## Easily Confused Pairs

| Looks alike | The distinction |
|---|---|
| Strategy vs State | Strategy: caller picks, strategies are unaware of each other. State: the state object picks the next state. |
| Strategy vs Bridge | Strategy swaps an algorithm; Bridge splits a whole abstraction from a whole implementation, both extensible. |
| Adapter vs Facade vs Proxy vs Decorator | Adapter *changes* an interface. Facade *simplifies* many into one. Proxy *keeps* the interface and controls access. Decorator *keeps* the interface and adds behavior, stackably. |
| Factory Method vs Abstract Factory | Factory Method: one product, chosen by subclass. Abstract Factory: a family of products that must match. |
| Builder vs Abstract Factory | Builder assembles one complex object step by step. Abstract Factory returns finished products immediately. |
| Mediator vs Observer | Mediator centralizes and *knows* the components. Observer broadcasts and the subject stays ignorant of subscribers. |
| Composite vs Decorator | Both wrap the component interface. Composite has many children; Decorator has exactly one and adds behavior. |
| Command vs Strategy | Command binds a receiver plus arguments into a reified, storable call. Strategy is a pluggable algorithm with no memory. |

## References

Full entries — intent, symptoms, minimal TypeScript shape, and the TypeScript
collapse — grouped by category:

- `references/creational.md` — Abstract Factory, Builder, Factory Method, Prototype, Singleton
- `references/structural.md` — Adapter, Bridge, Composite, Decorator, Facade, Flyweight, Proxy
- `references/behavioral.md` — Chain of Responsibility, Command, Iterator, Mediator, Memento, Observer, State, Strategy, Template Method, Visitor

Runnable Conceptual and RealWorld examples for every pattern live in the vendored
Refactoring.Guru repo at `~/dotfiles/skills/design-patterns-typescript/src/<Pattern>/`.
Run one with `ts-node src/<Pattern>/Conceptual/index.ts`.

Related per-pattern skills in this agent cover the JavaScript/web flavor of a few
of these (`singleton-pattern`, `factory-pattern`, `proxy-pattern`,
`observer-pattern`, `mediator-pattern`, `flyweight-pattern`, `prototype-pattern`,
`command-pattern`, `module-pattern`, `mixin-pattern`, `provider-pattern`). Use
this skill for *selection* and the classic OO structure; use those for
JS-idiomatic and React-specific implementations.

## Common Mistakes

- **Pattern-first design.** Deciding "this should be a Visitor" before the second use case exists.
- **Naming without structure.** A class called `PaymentFactory` with one `if` is not a factory.
- **Singleton as global state.** It hides dependencies and breaks test isolation. Prefer a module-scoped value or injected instance; see the `singleton-pattern` skill.
- **Interfaces with one implementation forever.** The indirection costs and buys nothing.
- **Porting Java literally.** `AbstractHandler` + 4 subclasses where an array of `(req) => Result | null` functions is clearer.
- **Depending on the concrete class anyway.** If the client does `new ConcreteStrategyA()` inline, the pattern bought nothing.
- **Stateful Strategy or Flyweight.** Both are meant to be shared; per-call state must be passed in as an argument.

## Red Flags — Stop and Reconsider

- "Let's make this extensible for the future" with no second case in hand
- The abstraction has exactly one implementation and no planned second
- You cannot state the change axis in one sentence
- The pattern adds a file per concrete case and each is 3 lines
- You are choosing between two patterns by which name sounds better

**All of these mean: write the direct code. Add the pattern when the second case arrives.**
