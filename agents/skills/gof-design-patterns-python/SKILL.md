---
name: gof-design-patterns-python
description: Use when writing or reviewing Python code that needs structure - an if/elif chain on a type or status, optional behaviors that stack in any order, undo/redo or audit history, a state machine with illegal transitions, "also notify X" arriving again, a subclass explosion, a third-party interface you cannot change, or a constructor with many optional arguments. Also use when about to hand-roll a mechanism, or when review asks "which pattern is this".
---

# GoF Design Patterns in Python

## Overview

Every abstraction in the code should be a **named pattern**, not a bespoke
mechanism invented on the spot. Ad-hoc structure is unreviewable: nobody can say
whether it is right, because there is nothing to check it against. A named
pattern comes with known roles, known failure modes, and a name a reviewer can
argue with.

**Core principle:** structure is either (a) direct code with no abstraction, or
(b) a pattern you can name. There is no third option. "I made a small pipeline of
steps" is option (b) done badly.

The 23 GoF patterns each buy **one axis of change** and pay with one indirection.
Name the axis first; pick the pattern that isolates exactly that axis.

Python has first-class functions, generators, protocols, dataclasses, and
`functools`. Many patterns collapse to a few lines here — the collapse is still
the pattern, and you still name it.

## The Rule

When you are about to introduce structure — a class hierarchy, a dispatch table,
a wrapper, a list of steps, a stack of history — do this, in order:

1. **State the change axis** in one sentence: *"`<what>` will change independently of `<what else>`."*
   Cannot write it? Write direct code and stop.
2. **Name the pattern** from the Symptom table below. If nothing fits, you have
   misread the axis — re-read the table before inventing anything.
3. **Read that entry** in `references/` and use its roles and method names
   (`Handler.handle`, `Command.execute`/`undo`, `Subject`/`Observer.update`,
   `Component`/`Decorator`, `Originator`/`Memento`). Canonical names are how the
   next reader recognizes it.
4. **Apply the Python collapse** when the entry has one — a generator, a
   `Protocol`, `functools.singledispatch`, a function strategy. Then say which
   pattern it is a collapse of.
5. **Say the name in the code and in your summary**: one line in the module or
   class docstring — `"""Decorator: stackable export post-processing."""` — and
   one line in what you report back.

Step 5 is not decoration. It is the check that step 2 actually happened.

## You Are About to Reinvent a Pattern

These are the moments the hand-rolled mechanism appears. Each already has a name.

| What you are about to write | Pattern you are reinventing |
|---|---|
| A list/tuple of step names plus an `enabled` dict, applied in a caller-supplied `order` | **Decorator** — order is the nesting; each layer keeps its own deps |
| Steps forced into one uniform signature, with parameters they ignore (`_key`, `**kwargs`) | **Decorator** — the uniform signature is the smell |
| A `_history` list of previous values, popped to "undo" | **Command** (undo of an action) or **Memento** (undo of state) |
| Undo that restores a field but not the side effects it caused | **Command** — `undo()` lives with the code that did it |
| Two dicts keyed by the same enum (`ALLOWED_OPS`, `ALLOWED_TRANSITIONS`) | **State** — or one table, never two that can drift |
| `global _instance` with a lock and double-checked `if is None` | **Singleton** — and in Python that is `@functools.cache` or a module-level object |
| A loop calling three hard-coded services after an event | **Observer** — the fourth is already coming |
| `isinstance` checks before every operation on a tree | **Composite** |
| A wrapper class that forwards most methods and caches one | **Proxy** — or `functools.cached_property` |
| A growing `if fmt == ...` returning different classes | **Factory Method**, or its registry-dict collapse |
| A class holding references to every sibling widget/service | **Mediator** |

## Symptom → Pattern

| Symptom | Pattern | Category |
|---|---|---|
| Client picks a concrete class by `if`/`match` on a tag | Factory Method | creational |
| Products must be created in matching families (backend, theme, vendor) | Abstract Factory | creational |
| Construction has many order-dependent or validating steps, or several representations | Builder | creational |
| Copying a configured object is cheaper than rebuilding it | Prototype | creational |
| Exactly one instance must exist process-wide | Singleton | creational |
| A third-party interface does not match yours | Adapter | structural |
| Subclasses multiply as `Kind × Platform` | Bridge | structural |
| A recursive tree where leaf and group must be treated alike | Composite | structural |
| Behavior must be stacked at runtime, in any order | Decorator | structural |
| A subsystem exposes 12 classes; callers need 2 calls | Facade | structural |
| Profiling shows millions of objects with duplicated immutable fields | Flyweight | structural |
| Lazy init, caching, access control, or logging on an object | Proxy | structural |
| A request passes through ordered, optional, short-circuiting handlers | Chain of Responsibility | behavioral |
| Actions must be undoable, queued, logged, or replayed | Command | behavioral |
| Traversal order varies, or collection internals must stay hidden | Iterator | behavioral |
| N components each talk to N others | Mediator | behavioral |
| Snapshot and restore private state | Memento | behavioral |
| Many objects must react to one object's events | Observer | behavioral |
| Behavior changes wholesale with status; illegal transitions matter | State | behavioral |
| One job, several interchangeable algorithms | Strategy | behavioral |
| Fixed algorithm skeleton, varying steps | Template Method | behavioral |
| New operations added often over stable node types | Visitor | behavioral |

## Python Collapses

The pattern is still the pattern; the Python form is smaller. Name it anyway.

| Pattern | Idiomatic Python form |
|---|---|
| Strategy | a function or `functools.partial` passed in (`sorted(key=...)`) |
| Iterator | `__iter__` + `yield from`; generators |
| Visitor | `functools.singledispatch`, or `match` with class patterns |
| Factory Method | `dict[str, Callable[..., Product]]` registry |
| Singleton | module-level object, or `@functools.cache` factory |
| Prototype | `copy.deepcopy`, `dataclasses.replace` |
| Builder | keyword args + `@dataclass` defaults (unless steps must sequence) |
| Decorator (functions) | `@functools.wraps` decorators |
| Proxy | `functools.cached_property`, `@functools.cache`, `__getattr__` forwarding |
| Chain of Responsibility | a list of `Callable[[Req], Res | None]` walked in a loop |
| Observer | Django `Signal`, `blinker`, or a `list[Observer]` / `WeakSet` |
| Adapter | one conversion function, when there is no state |

Use `typing.Protocol` for the participant interfaces — structural typing means
adapters and test doubles need no base class. Use `abc.ABC` only when you supply
shared implementation (Template Method, Composite).

## Easily Confused Pairs

| Looks alike | The distinction |
|---|---|
| Strategy vs State | Strategy: the caller picks, strategies ignore each other. State: the state object picks the next state. |
| Strategy vs Bridge | Strategy swaps an algorithm. Bridge splits two open hierarchies. |
| Adapter vs Facade vs Proxy vs Decorator | Adapter *changes* the interface. Facade *simplifies* many into one. Proxy *keeps* it and controls access. Decorator *keeps* it and adds behavior, stackably. |
| Factory Method vs Abstract Factory | One product chosen by subclass, vs a family that must match. |
| Builder vs Abstract Factory | Builder assembles one object step by step; Abstract Factory returns finished products. |
| Command vs Memento | Command reverses an *action* (and its side effects); Memento restores a *snapshot*. Undo of a workflow usually needs both. |
| Mediator vs Observer | Mediator centralizes and knows the components; Observer broadcasts and the subject stays ignorant. |
| Composite vs Decorator | Composite has many children; Decorator has exactly one and adds behavior. |
| Template Method vs Strategy | Inheritance fills steps vs composition supplies them. Prefer Strategy when one step varies. |

## When No Pattern Is the Answer

Naming a pattern is required *when you introduce structure*. Not introducing
structure is always allowed and often right:

- **One implementation exists and no second is in hand.** Write the function.
- **The variation is data, not behavior.** Configure it; do not subclass it.
- **The stdlib already is the pattern.** `sorted(key=...)`, `contextlib`,
  `@functools.cache`, `Signal` — use them and name what they are.
- **You are only renaming.** Calling a class `PaymentFactory` around one `if`
  changes nothing.

Say "no pattern — direct code" explicitly. That is a valid, reportable answer.

## References

Intent, symptoms, canonical Python shape, and the collapse for each pattern:

- `references/creational.md` — Abstract Factory, Builder, Factory Method, Prototype, Singleton
- `references/structural.md` — Adapter, Bridge, Composite, Decorator, Facade, Flyweight, Proxy
- `references/behavioral.md` — Chain of Responsibility, Command, Iterator, Mediator, Memento, Observer, State, Strategy, Template Method, Visitor

Runnable Conceptual examples for all 22 patterns live in the vendored
Refactoring.Guru repo at `~/dotfiles/skills/design-patterns-python/src/<Pattern>/Conceptual/`.
Run one with `python ~/dotfiles/skills/design-patterns-python/src/<Pattern>/Conceptual/main.py`;
each directory has an `Output.txt` with the expected result.

## Common Mistakes

- **Inventing a mechanism instead of naming one.** An ordered list of steps with
  an `enabled` map *is* Decorator, written worse — the layers cannot carry their
  own dependencies, so every parameter leaks into every step.
- **Undo built from stored values.** Popping a previous state restores a field
  but not the charge, the email, or the shipping label. Undo belongs in `Command.undo`.
- **Undo that bypasses the transition rules.** If undo can reach a state the
  state machine forbids, the machine is not the source of truth any more.
- **Two parallel tables keyed by the same enum.** They drift on the first change.
  One table, or State classes.
- **Hand-rolled double-checked locking.** Module-level objects and
  `@functools.cache` are already thread-safe and shorter.
- **Porting Java literally.** `AbstractHandler` plus four subclasses where a list
  of functions reads better.
- **Stateful Strategy or Flyweight.** Both are shared; per-call state is an argument.
- **Depending on the concrete class anyway.** If the caller does
  `ConcreteStrategyA()` inline, the indirection bought nothing.
- **Singleton as global state.** Hides dependencies, breaks test isolation. Inject
  the instance at the composition root.

## Red Flags — Stop and Name It

- You are writing a mechanism you cannot name in one word
- You reach for `**kwargs` so several steps can share a signature
- "Extensible for the future" with no second case in hand
- The abstraction has one implementation and no planned second
- You cannot state the change axis in one sentence
- You are choosing between two patterns by which name sounds better

**First four: name the pattern from the table, or write direct code. Last two:
write direct code — the pattern arrives with the second case.**
