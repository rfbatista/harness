# Behavioral Patterns

They isolate **how responsibility and control flow move between objects at
runtime**.

Repo examples: `~/dotfiles/skills/design-patterns-typescript/src/<Pattern>/Conceptual/index.ts`

---

## Chain of Responsibility

**Intent:** pass a request along a chain of handlers until one handles it.

**Reach for it when**
- Middleware-shaped processing: validate → authenticate → rate-limit → cache → handle
- The set and order of checks must be configurable at runtime
- Any handler may legitimately stop the chain

**Shape**

```ts
interface Handler<Request = string, Result = string> {
  setNext(handler: Handler<Request, Result>): Handler<Request, Result>;
  handle(request: Request): Result | null;
}

abstract class AbstractHandler implements Handler {
  private nextHandler!: Handler;

  // Returning the handler enables chaining: a.setNext(b).setNext(c)
  setNext(handler: Handler): Handler {
    this.nextHandler = handler;
    return handler;
  }

  handle(request: string): string | null {
    return this.nextHandler ? this.nextHandler.handle(request) : null;
  }
}

class MonkeyHandler extends AbstractHandler {
  handle(request: string): string | null {
    if (request === 'Banana') return `Monkey: I'll eat the ${request}.`;
    return super.handle(request);   // not mine — pass it on
  }
}
```

The client must handle the "nobody handled it" case; `handle` returning `null` at
the end of the chain is not an error.

**Skip it when** the order is fixed and short — an array of predicate functions
reduced in sequence says the same thing without the class hierarchy:

```ts
const handlers: Array<(r: Request) => Result | null> = [checkAuth, checkQuota, serve];
const result = handlers.reduce<Result | null>((acc, h) => acc ?? h(req), null);
```

**Repo:** `src/ChainOfResponsibility/`

---

## Command

**Intent:** turn a request into a standalone object carrying everything needed to
perform it.

**Reach for it when**
- Undo/redo, or an operation log you can replay
- Operations must be queued, scheduled, retried, or sent over the wire
- The invoker (button, menu item, keybinding) must not know the receiver

**Shape**

```ts
interface Command {
  execute(): void;
  // add undo(): void when you need reversibility
}

class SimpleCommand implements Command {
  constructor(private payload: string) {}
  execute(): void { console.log(`printing ${this.payload}`); }
}

class ComplexCommand implements Command {
  // Binds the receiver AND the arguments — that is what makes it storable.
  constructor(private receiver: Receiver, private a: string, private b: string) {}
  execute(): void {
    this.receiver.doSomething(this.a);
    this.receiver.doSomethingElse(this.b);
  }
}

class Invoker {
  private onStart?: Command;
  setOnStart(command: Command): void { this.onStart = command; }
  doSomethingImportant(): void { this.onStart?.execute(); }
}
```

For undo, each command captures whatever it needs to reverse itself — often a
Memento of the receiver's prior state.

**Skip it when** you only need deferred invocation with no undo, no queue, and no
logging. A closure `() => receiver.doSomething(a, b)` is a command object with
less ceremony. See also the `command-pattern` skill.

**Repo:** `src/Command/`

---

## Iterator

**Intent:** traverse a collection without exposing its internal representation.

**Reach for it when**
- Several traversal orders over one collection (forward, reverse, filtered, breadth/depth-first)
- The underlying structure (tree, graph, paged API) must stay hidden
- Traversal state must be independent, so two walks can run at once

**Shape** — in TypeScript, implement the built-in protocol rather than a custom
interface, so `for...of`, spread, and destructuring all work:

```ts
class WordsCollection implements Iterable<string> {
  private items: string[] = [];
  addItem(item: string): void { this.items.push(item); }

  *[Symbol.iterator](): Iterator<string> {
    yield* this.items;
  }

  // Alternative traversals get their own generator methods.
  *reversed(): IterableIterator<string> {
    for (let i = this.items.length - 1; i >= 0; i--) yield this.items[i];
  }
}

for (const word of collection) console.log(word);
for (const word of collection.reversed()) console.log(word);
```

The repo's version predates this and hand-rolls `current/next/key/valid/rewind`;
useful for seeing the mechanics, but do not copy it into new code.

**Skip it when** the collection is an array. `map`/`filter`/`for...of` already are
the iterator.

**Repo:** `src/Iterator/`

---

## Mediator

**Intent:** put the interaction logic between components into one object so the
components stop referencing each other.

**Reach for it when**
- N components each hold references to N others
- Reusing one component drags in half the others
- Coordination rules ("when A changes, refresh B and validate C") are scattered across components

**Shape**

```ts
interface Mediator {
  notify(sender: object, event: string): void;
}

class ConcreteMediator implements Mediator {
  constructor(private c1: Component1, private c2: Component2) {
    this.c1.setMediator(this);
    this.c2.setMediator(this);
  }

  // All cross-component rules live here.
  notify(_sender: object, event: string): void {
    if (event === 'A') this.c2.doC();
    if (event === 'D') { this.c1.doB(); this.c2.doC(); }
  }
}

class BaseComponent {
  constructor(protected mediator?: Mediator) {}
  setMediator(mediator: Mediator): void { this.mediator = mediator; }
}
```

Components know only the `Mediator` interface, never each other.

**Skip it when** there are two or three components — the mediator becomes a god
object faster than you expect. Watch for it growing an `if` per event pair; that
is the signal to split it or move to an event bus (Observer). See the
`mediator-pattern` skill.

**Repo:** `src/Mediator/`

---

## Memento

**Intent:** capture and restore an object's internal state without violating its
encapsulation.

**Reach for it when**
- Undo/redo, checkpoints, transaction rollback, or a "revert to draft" feature
- The state to snapshot includes private fields
- The history keeper must not be able to read or tamper with the snapshot

**Shape**

```ts
// The caretaker sees only this narrow interface — metadata, not state.
interface Memento {
  getName(): string;
  getDate(): string;
}

class ConcreteMemento implements Memento {
  private date = new Date().toISOString();
  constructor(private state: string) {}
  getState(): string { return this.state; }     // only the Originator calls this
  getName(): string { return `${this.date} / (${this.state.slice(0, 9)}...)`; }
  getDate(): string { return this.date; }
}

class Originator {
  constructor(private state: string) {}
  save(): Memento { return new ConcreteMemento(this.state); }
  restore(memento: Memento): void {
    this.state = (memento as ConcreteMemento).getState();
  }
}

class Caretaker {
  private mementos: Memento[] = [];
  constructor(private originator: Originator) {}
  backup(): void { this.mementos.push(this.originator.save()); }
  undo(): void {
    const memento = this.mementos.pop();
    if (memento) this.originator.restore(memento);
  }
}
```

The narrow `Memento` interface is the pattern — without it the caretaker could
read and rewrite the originator's private state.

**Skip it when** the state is a small immutable value. Keep a stack of frozen
snapshots (or the persistent state from your store) and drop the ceremony.
Watch memory: snapshotting a large object on every keystroke adds up.

**Repo:** `src/Memento/`

---

## Observer

**Intent:** notify a set of dependents automatically when a subject changes state.

**Reach for it when**
- Many parts of the app must react to one event and the publisher must not know them
- Subscribers come and go at runtime
- You are building an event bus, store subscription, or reactive binding

**Shape**

```ts
interface Observer {
  update(subject: Subject): void;
}

interface Subject {
  attach(observer: Observer): void;
  detach(observer: Observer): void;
  notify(): void;
}

class ConcreteSubject implements Subject {
  public state = 0;
  private observers: Observer[] = [];

  attach(observer: Observer): void {
    if (!this.observers.includes(observer)) this.observers.push(observer);
  }
  detach(observer: Observer): void {
    const i = this.observers.indexOf(observer);
    if (i !== -1) this.observers.splice(i, 1);
  }
  notify(): void {
    // Copy first if a handler may detach during iteration.
    for (const observer of [...this.observers]) observer.update(this);
  }

  someBusinessLogic(): void {
    this.state = Math.floor(Math.random() * 11);
    this.notify();
  }
}
```

Always return or expose an unsubscribe path — an observer that is never detached
is a memory leak, and the commonest bug in this pattern.

**Skip it when** the platform gives you one: `EventTarget`/`addEventListener`,
Node's `EventEmitter`, or a signals/store library. See the `observer-pattern` skill.

**Repo:** `src/Observer/`

---

## State

**Intent:** let an object change its behavior when its internal state changes, as
if it had changed class.

**Reach for it when**
- Every method starts with the same `switch (this.status)`
- Transitions are a real machine with illegal moves (draft → review → published)
- State-specific behavior is large enough to be worth its own class

**Shape**

```ts
abstract class State {
  protected context!: Context;
  setContext(context: Context) { this.context = context; }
  abstract handle1(): void;
  abstract handle2(): void;
}

class Context {
  private state!: State;
  constructor(state: State) { this.transitionTo(state); }

  transitionTo(state: State): void {
    this.state = state;
    this.state.setContext(this);
  }

  request1(): void { this.state.handle1(); }
  request2(): void { this.state.handle2(); }
}

class ConcreteStateA extends State {
  handle1(): void { this.context.transitionTo(new ConcreteStateB()); }
  handle2(): void { /* ... */ }
}
```

State vs Strategy: the states know each other and pick the successor; strategies
never do — the client swaps them.

**Skip it when** behavior differences are small. A discriminated union plus a
transition table is more compact and gets exhaustiveness checking:

```ts
const transitions = {
  draft:     { submit: 'review' },
  review:    { approve: 'published', reject: 'draft' },
  published: {},
} as const satisfies Record<Status, Partial<Record<Action, Status>>>;
```

**Repo:** `src/State/`

---

## Strategy

**Intent:** define a family of interchangeable algorithms and make them swappable
at runtime.

**Reach for it when**
- One task, several algorithms: sort orders, pricing rules, routing, compression
- A `switch` on an algorithm name keeps growing
- The choice must be made at runtime by config, A/B test, or user setting

**Shape**

```ts
interface Strategy {
  doAlgorithm(data: string[]): string[];
}

class Context {
  constructor(private strategy: Strategy) {}
  setStrategy(strategy: Strategy) { this.strategy = strategy; }
  doSomeBusinessLogic(): void {
    console.log(this.strategy.doAlgorithm(['a', 'b', 'c']).join(','));
  }
}

class ConcreteStrategyA implements Strategy {
  doAlgorithm(data: string[]): string[] { return data.sort(); }
}
```

Strategies must be stateless and unaware of each other; anything per-call comes in
as an argument.

**Skip it when** the strategy is one method. In TypeScript that is a function type,
and the "pattern" is a parameter:

```ts
type Sorter = (data: string[]) => string[];
function process(data: string[], sort: Sorter) { return sort(data); }
```

**Repo:** `src/Strategy/`

---

## Template Method

**Intent:** define the skeleton of an algorithm in a base class and let subclasses
override individual steps.

**Reach for it when**
- Several procedures share a fixed sequence and differ in two or three steps
- The order of steps must not be up to the subclass
- Duplicated pipelines differ only in the middle (parse → *transform* → *validate* → save)

**Shape**

```ts
abstract class AbstractClass {
  // Not overridable: the skeleton is the contract.
  public templateMethod(): void {
    this.baseOperation1();
    this.requiredOperation1();
    this.hook1();              // optional extension point
    this.requiredOperation2();
  }

  protected baseOperation1(): void { /* shared work */ }

  protected abstract requiredOperation1(): void;
  protected abstract requiredOperation2(): void;

  protected hook1(): void {}   // empty by default — subclasses may override
}
```

Hooks are the difference between a rigid template and a usable one: required steps
are `abstract`, optional ones are empty methods.

**Skip it when** inheritance is the only reason for the base class. Passing the
varying steps as callbacks gives the same skeleton with composition:

```ts
function runPipeline(steps: { transform(x: In): Mid; validate(x: Mid): void }) { /* ... */ }
```

**Repo:** `src/TemplateMethod/`

---

## Visitor

**Intent:** add new operations to a stable set of object types without modifying
those types.

**Reach for it when**
- The node types are stable but operations are added constantly: AST compilers, linters, exporters, interpreters
- The operation needs type-specific behavior across a whole hierarchy
- The behavior does not belong in the node classes (report generation, serialization)

**Shape**

```ts
interface Visitor {
  visitConcreteComponentA(element: ConcreteComponentA): void;
  visitConcreteComponentB(element: ConcreteComponentB): void;
}

interface Component {
  accept(visitor: Visitor): void;
}

class ConcreteComponentA implements Component {
  // Double dispatch: the element picks the visitor method for its own type.
  accept(visitor: Visitor): void { visitor.visitConcreteComponentA(this); }
  exclusiveMethodOfConcreteComponentA(): string { return 'A'; }
}

class ConcreteVisitor1 implements Visitor {
  visitConcreteComponentA(element: ConcreteComponentA): void {
    console.log(`${element.exclusiveMethodOfConcreteComponentA()} + Visitor1`);
  }
  visitConcreteComponentB(element: ConcreteComponentB): void { /* ... */ }
}
```

The trade-off is fixed: adding an *operation* is cheap (one new visitor); adding a
*node type* is expensive (every visitor must change). Use it only when that
asymmetry matches yours.

**Skip it when** node types change more often than operations, or when a
discriminated union plus a `switch` gives you the same dispatch with compile-time
exhaustiveness and no `accept` boilerplate:

```ts
type Expr =
  | { kind: 'literal'; value: number }
  | { kind: 'add'; left: Expr; right: Expr };

function evaluate(node: Expr): number {
  switch (node.kind) {
    case 'literal': return node.value;
    case 'add':     return evaluate(node.left) + evaluate(node.right);
  }
}
```

This is the TypeScript-idiomatic default; reach for classic Visitor mainly when
the node types are classes you do not own.

**Repo:** `src/Visitor/`
