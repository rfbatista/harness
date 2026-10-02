# Creational Patterns

They isolate **what gets instantiated** from the code that uses it.

Repo examples: `~/dotfiles/skills/design-patterns-typescript/src/<Pattern>/Conceptual/index.ts`

---

## Factory Method

**Intent:** let a subclass decide which concrete product to instantiate, while the
base class owns the algorithm that uses it.

**Reach for it when**
- A `switch (type)` picks a class and grows with every new type
- The base class has useful logic that must run against any product
- A plugin or vendor supplies the product type

**Shape**

```ts
interface Product {
  operation(): string;
}

abstract class Creator {
  protected abstract factoryMethod(): Product;   // the hook

  // Shared algorithm — unchanged when a new product arrives.
  public someOperation(): string {
    return `Creator worked with ${this.factoryMethod().operation()}`;
  }
}

class ConcreteCreator1 extends Creator {
  protected factoryMethod(): Product {
    return new ConcreteProduct1();
  }
}
```

**Skip it when** the creator has no shared logic. Then it is just a `Record<Kind, () => Product>`
lookup, which is smaller and exhaustively checked:

```ts
const products = {
  csv: () => new CsvProduct(),
  json: () => new JsonProduct(),
} satisfies Record<Kind, () => Product>;
```

**Repo:** `src/FactoryMethod/`

---

## Abstract Factory

**Intent:** create *families* of related products without naming their concrete
classes, so that mismatched members can never be combined.

**Reach for it when**
- Products must match: Victorian chair + Victorian sofa, never a mix
- Swapping platform/theme/vendor swaps several types at once
- The constraint "these must come from the same family" is currently enforced by convention

**Shape**

```ts
interface AbstractFactory {
  createProductA(): AbstractProductA;
  createProductB(): AbstractProductB;
}

class ConcreteFactory1 implements AbstractFactory {
  createProductA(): AbstractProductA { return new ConcreteProductA1(); }
  createProductB(): AbstractProductB { return new ConcreteProductB1(); }
}

// The client never names a concrete class.
function clientCode(factory: AbstractFactory) {
  const a = factory.createProductA();
  const b = factory.createProductB();
  return b.anotherUsefulFunctionB(a);
}
```

**Skip it when** there is only one product type — that is Factory Method. Or when
the family never has to match, in which case independent factories are looser and
just as safe.

**Repo:** `src/AbstractFactory/`

---

## Builder

**Intent:** construct a complex object step by step, so the same construction
sequence can yield different representations.

**Reach for it when**
- The constructor takes many optional or order-dependent parameters
- Several recipes exist (minimal / full-featured / custom)
- A telescoping set of overloads is appearing

**Shape**

```ts
interface Builder {
  producePartA(): void;
  producePartB(): void;
}

class ConcreteBuilder implements Builder {
  private product!: Product;
  constructor() { this.reset(); }
  reset(): void { this.product = new Product(); }
  producePartA(): void { this.product.parts.push('PartA'); }
  producePartB(): void { this.product.parts.push('PartB'); }

  // Hands over the result and starts a fresh product.
  getProduct(): Product {
    const result = this.product;
    this.reset();
    return result;
  }
}

// Optional: the Director encodes reusable recipes.
class Director {
  constructor(private builder: Builder) {}
  buildMinimalViableProduct(): void { this.builder.producePartA(); }
  buildFullFeaturedProduct(): void {
    this.builder.producePartA();
    this.builder.producePartB();
  }
}
```

The Director is optional — the client can drive the builder directly for one-off
configurations. Note `getProduct()` resets: a builder that forgets this leaks
state into the next product.

**Skip it when** the object is a plain bag of values. An options object with
optional properties is clearer and type-checked:

```ts
function createReport(opts: { title: string; footer?: string; charts?: Chart[] }) { /* ... */ }
```

Prefer a fluent builder (`return this`) only when steps have real ordering rules;
otherwise it is chaining for its own sake.

**Repo:** `src/Builder/` (also has a `Book/` variant)

---

## Prototype

**Intent:** create new objects by cloning an existing instance rather than
constructing from scratch.

**Reach for it when**
- Construction is expensive (parsed config, warmed cache, DB round-trip)
- The concrete class is not known at the call site, but an instance is
- You need a configured template object duplicated many times

**Shape**

```ts
class Prototype {
  public primitive!: number;
  public component!: object;
  public circularReference!: ComponentWithBackReference;

  public clone(): this {
    const clone = Object.create(this);
    // Deep-copy the parts that must not be shared.
    clone.component = Object.create(this.component);
    // Re-point back-references at the clone, not the original.
    clone.circularReference = new ComponentWithBackReference(clone);
    return clone;
  }
}
```

The whole difficulty is the shallow/deep boundary: decide per field which
references may be shared, and re-point every back-reference at the clone.

**Skip it when** the object is JSON-shaped and has no back-references or class
identity to preserve — `structuredClone(obj)` covers it.

**Repo:** `src/Prototype/`

---

## Singleton

**Intent:** guarantee one instance and give it a global access point.

**Reach for it when**
- A real resource must not be duplicated (connection pool, hardware handle)
- Two instances would be an outright bug, not just waste

**Shape**

```ts
class Singleton {
  static #instance: Singleton;
  private constructor() {}

  public static get instance(): Singleton {
    if (!Singleton.#instance) {
      Singleton.#instance = new Singleton();
    }
    return Singleton.#instance;
  }
}
```

**Skip it when** — usually. In TypeScript an ES module is already a singleton, so
`export const db = createDb()` gives the same sharing with less ceremony. Both
forms hide the dependency from callers and share mutable state across tests;
injecting the instance keeps tests isolated. Treat Singleton as a last resort and
read the `singleton-pattern` skill for the JavaScript-specific trade-offs.

**Repo:** `src/Singleton/`
