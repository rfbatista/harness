# Structural Patterns

They isolate **how objects are composed and wired**, so a class hierarchy does not
have to grow to absorb every combination.

Repo examples: `~/dotfiles/skills/design-patterns-typescript/src/<Pattern>/Conceptual/index.ts`

---

## Adapter

**Intent:** translate one interface into another so incompatible classes can work
together.

**Reach for it when**
- A library, legacy module, or vendor SDK has the capability but the wrong shape
- You cannot change either side (third-party code, or code with many other callers)
- Two subsystems disagree on units, naming, or return shape

**Shape**

```ts
class Target {                       // what the client expects
  request(): string { return 'default behavior'; }
}

class Adaptee {                      // what you actually have
  specificRequest(): string { return '.eetpadA eht fo roivaheb laicepS'; }
}

class Adapter extends Target {
  constructor(private adaptee: Adaptee) { super(); }
  request(): string {
    // All translation lives here, and only here.
    return `(TRANSLATED) ${this.adaptee.specificRequest().split('').reverse().join('')}`;
  }
}
```

Object adapters (composition, as above) are almost always right in TypeScript;
class adapters via `extends` only work when the target is a class you may subclass.

**Skip it when** you own both sides — fix the interface instead of papering over it.
For a one-method interface, a wrapper function beats a class.

**Repo:** `src/Adapter/`

---

## Bridge

**Intent:** split an abstraction from its implementation so both can vary
independently.

**Reach for it when**
- Subclasses multiply as a Cartesian product: `RemoteControl × Device`, `Shape × Renderer`, `Report × Format`
- Adding one platform forces you to add N classes
- Two dimensions of change are currently tangled in one hierarchy

**Shape**

```ts
interface Implementation {
  operationImplementation(): string;
}

class Abstraction {
  constructor(protected implementation: Implementation) {}
  operation(): string {
    return `Abstraction: ${this.implementation.operationImplementation()}`;
  }
}

// Extends along the abstraction axis only — implementations are untouched.
class ExtendedAbstraction extends Abstraction {
  operation(): string {
    return `Extended: ${this.implementation.operationImplementation()}`;
  }
}
```

Bridge vs Strategy: Strategy swaps one algorithm behind a stable caller; Bridge
expects *both* sides to grow their own hierarchies.

**Skip it when** one axis is not actually growing. Two dimensions where one has a
single member is just a Strategy — or plain composition.

**Repo:** `src/Bridge/`

---

## Composite

**Intent:** treat individual objects and compositions of objects uniformly through
one interface.

**Reach for it when**
- The domain is genuinely a tree: files/folders, orders/boxes, groups/shapes, menu trees
- The client keeps checking `Array.isArray(node)` or `node.children?.length`
- An operation must aggregate recursively over the tree

**Shape**

```ts
abstract class Component {
  protected parent: Component | null = null;
  setParent(parent: Component | null) { this.parent = parent; }
  add(_component: Component): void {}       // no-op on leaves
  remove(_component: Component): void {}
  isComposite(): boolean { return false; }
  abstract operation(): string;
}

class Leaf extends Component {
  operation(): string { return 'Leaf'; }
}

class Composite extends Component {
  protected children: Component[] = [];
  add(component: Component): void {
    this.children.push(component);
    component.setParent(this);
  }
  isComposite(): boolean { return true; }
  operation(): string {
    // Recursion is the whole point: children may be leaves or composites.
    return `Branch(${this.children.map(c => c.operation()).join('+')})`;
  }
}
```

The trade-off GoF names explicitly: putting `add`/`remove` on `Component` keeps the
client uniform but lets you call `add` on a leaf. Moving them to `Composite` is
type-safe but forces the client to narrow.

**Skip it when** the structure is flat, or when a discriminated union plus a
recursive function is clearer:

```ts
type TreeNode = { kind: 'leaf'; value: number } | { kind: 'branch'; children: TreeNode[] };
```

**Repo:** `src/Composite/`

---

## Decorator

**Intent:** attach responsibilities to an object dynamically by wrapping it in
objects of the same interface.

**Reach for it when**
- Behavior must be layered at runtime and in varying order: compress → encrypt → log
- Subclassing would need one class per combination
- The additions are optional and independently useful

**Shape**

```ts
interface Component {
  operation(): string;
}

class ConcreteComponent implements Component {
  operation(): string { return 'ConcreteComponent'; }
}

class Decorator implements Component {
  constructor(protected component: Component) {}
  operation(): string { return this.component.operation(); }
}

class ConcreteDecoratorA extends Decorator {
  operation(): string { return `A(${super.operation()})`; }
}

// Order matters and is chosen at runtime.
const decorated = new ConcreteDecoratorB(new ConcreteDecoratorA(new ConcreteComponent()));
```

Decorator vs Proxy: identical structure, different intent. A Decorator *adds* behavior
the client asked for; a Proxy *controls* access to something the client thinks it has directly.

**Skip it when** the interface is one function — compose higher-order functions
instead: `withLogging(withRetry(fetchUser))`.

**Repo:** `src/Decorator/`

---

## Facade

**Intent:** give a simplified interface to a complex subsystem.

**Reach for it when**
- Callers must know the initialization order of several subsystem classes
- The same 20-line setup sequence is copy-pasted across call sites
- You want one seam to isolate the app from a library you may replace

**Shape**

```ts
class Facade {
  constructor(
    protected subsystem1: Subsystem1 = new Subsystem1(),
    protected subsystem2: Subsystem2 = new Subsystem2(),
  ) {}

  // One call replaces the whole choreography.
  operation(): string {
    return [
      this.subsystem1.operation1(),
      this.subsystem2.operation1(),
      this.subsystem1.operationN(),
      this.subsystem2.operationZ(),
    ].join('');
  }
}
```

Accepting the subsystems as constructor arguments (rather than always constructing
them) keeps the facade testable.

**Skip it when** the subsystem already has a reasonable entry point. A facade that
forwards one call to one method is dead weight. Also resist the god-facade: split
into several facades before it accumulates every method in the subsystem.

**Repo:** `src/Facade/`

---

## Flyweight

**Intent:** share the invariant part of state across many objects to fit more of
them in memory.

**Reach for it when**
- You have tens of thousands of objects and profiling shows memory is the problem
- Most fields are duplicated across instances (sprite, font, tile, particle type)
- Extrinsic state can be passed in per call instead of stored

**Shape**

```ts
class Flyweight {
  constructor(private sharedState: string[]) {}   // intrinsic — immutable, shared
  operation(uniqueState: string[]): void {         // extrinsic — passed in per call
    console.log(`shared ${JSON.stringify(this.sharedState)} unique ${JSON.stringify(uniqueState)}`);
  }
}

class FlyweightFactory {
  private flyweights: Record<string, Flyweight> = {};
  private getKey(state: string[]): string { return state.join('_'); }

  // The factory is what guarantees sharing.
  getFlyweight(sharedState: string[]): Flyweight {
    const key = this.getKey(sharedState);
    this.flyweights[key] ??= new Flyweight(sharedState);
    return this.flyweights[key];
  }
}
```

The intrinsic state must be immutable — mutating a shared flyweight corrupts every
context using it.

**Skip it when** you have not measured. This pattern trades readability for bytes;
without a memory problem it is pure cost. For simple value reuse an interning
`Map` is enough.

**Repo:** `src/Flyweight/`

---

## Proxy

**Intent:** stand in for another object, with the same interface, to control access
to it.

**Reach for it when**
- Lazy initialization of an expensive object (virtual proxy)
- Access control, quota, or auth checks before delegating (protection proxy)
- Caching, logging, or reference counting around every call
- The real subject lives elsewhere (remote proxy)

**Shape**

```ts
interface Subject {
  request(): void;
}

class RealSubject implements Subject {
  request(): void { console.log('handling request'); }
}

class ProxySubject implements Subject {
  constructor(private realSubject: RealSubject) {}
  request(): void {
    if (this.checkAccess()) {
      this.realSubject.request();
      this.logAccess();
    }
  }
  private checkAccess(): boolean { return true; }
  private logAccess(): void {}
}
```

The proxy must implement the *same* interface — that is what lets it be dropped in
without touching the client.

**Skip it when** you want cross-cutting interception over arbitrary properties;
JavaScript's built-in `new Proxy(target, handler)` does it generically with `get`,
`set`, and `has` traps. See the `proxy-pattern` skill. Note the built-in adds a
real performance cost on hot paths.

**Repo:** `src/Proxy/`
