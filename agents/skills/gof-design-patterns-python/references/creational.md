# Creational Patterns

They isolate **what gets instantiated** from the code that uses it.

Repo examples: `~/dotfiles/skills/design-patterns-python/src/<Pattern>/Conceptual/main.py`

---

## Factory Method

**Intent:** let a subclass decide which concrete product to instantiate, while the
base class owns the algorithm that uses it.

**Reach for it when**
- An `if fmt == ...` chain picks a class and grows with every new type
- The base class has useful logic that must run against any product
- A plugin or vendor supplies the product type

**Shape**

```python
from abc import ABC, abstractmethod

class Product(ABC):
    @abstractmethod
    def operation(self) -> str: ...

class Creator(ABC):
    @abstractmethod
    def factory_method(self) -> Product: ...       # the hook

    def some_operation(self) -> str:               # shared algorithm
        return f"Creator worked with {self.factory_method().operation()}"

class CsvCreator(Creator):
    def factory_method(self) -> Product:
        return CsvProduct()
```

**Skip it when** the creator has no shared logic. Then it is a registry dict, which
is smaller and testable:

```python
PRODUCTS: dict[str, Callable[[], Product]] = {"csv": CsvProduct, "json": JsonProduct}
product = PRODUCTS[fmt]()
```

Populate the registry with a decorator or `__init_subclass__` when third parties
register products; keep the dict literal when you own every entry.

**Repo:** `src/FactoryMethod/`

---

## Abstract Factory

**Intent:** create *families* of related products without naming their concrete
classes, so mismatched members can never be combined.

**Reach for it when**
- Products must match: Postgres connection + Postgres dialect + Postgres migrator, never a mix
- Swapping backend/theme/vendor swaps several types at once
- "These must come from the same family" is currently enforced by convention

**Shape**

```python
class GuiFactory(Protocol):
    def create_button(self) -> Button: ...
    def create_checkbox(self) -> Checkbox: ...

class MacFactory:
    def create_button(self) -> Button: return MacButton()
    def create_checkbox(self) -> Checkbox: return MacCheckbox()

def client(factory: GuiFactory) -> None:   # never names a concrete class
    factory.create_button().paint()
```

**Skip it when** there is one family, or the members are independent. A
`@dataclass(frozen=True)` bundling three already-built objects is a config object,
not an Abstract Factory — and is the right answer when nothing is deferred.

**Repo:** `src/AbstractFactory/`

---

## Builder

**Intent:** assemble a complex object step by step, so the same steps can produce
different representations.

**Reach for it when**
- Construction needs many optional or order-dependent steps
- The same build sequence must yield different products (HTML vs plain text report)
- A Director encodes recipes clients should not repeat

**Shape**

```python
class QueryBuilder:
    def __init__(self) -> None:
        self.reset()

    def reset(self) -> None:
        self._parts: list[str] = []

    def where(self, clause: str) -> "QueryBuilder":
        self._parts.append(f"WHERE {clause}")
        return self                       # chaining is optional, not the pattern

    def build(self) -> Query:
        query, _ = Query(" ".join(self._parts)), self.reset()
        return query
```

**Skip it when** the object is just data with defaults. Python has keyword
arguments and `@dataclass` — a 12-field frozen dataclass with defaults beats a
builder, and `dataclasses.replace(cfg, timeout=5)` covers "same thing, one field
different". Reach for Builder only when steps must run in sequence, validate each
other, or produce more than one representation.

**Repo:** `src/Builder/`

---

## Prototype

**Intent:** produce new objects by copying an existing configured instance.

**Reach for it when**
- Building from scratch is expensive but copying is cheap
- The exact class is unknown at runtime (you hold an instance, not a type)
- Objects act as configured templates that get tweaked per use

**Shape**

```python
import copy

class Node:
    def clone(self) -> "Node":
        return copy.deepcopy(self)        # override __deepcopy__ for cycles/handles
```

**Skip it when** `copy.copy` / `copy.deepcopy` on a dataclass already does it —
that *is* the pattern, and a `clone()` method that only forwards to `deepcopy`
adds nothing. Define `__copy__`/`__deepcopy__` when the object holds a socket,
file handle, or cache that must not be duplicated.

**Repo:** `src/Prototype/`

---

## Singleton

**Intent:** guarantee one instance and a global access point to it.

**Reach for it when**
- One process-wide coordinator genuinely must exist (a connection pool, a registry)
- Two instances would be a correctness bug, not just waste

**Shape (metaclass, thread-safe)**

```python
import threading

class SingletonMeta(type):
    _instances: dict[type, object] = {}
    _lock = threading.Lock()

    def __call__(cls, *args, **kwargs):
        with cls._lock:
            if cls not in cls._instances:
                cls._instances[cls] = super().__call__(*args, **kwargs)
        return cls._instances[cls]

class Pool(metaclass=SingletonMeta): ...
```

**Skip it when** — almost always. In Python a module *is* a singleton: a
module-level object is created once on first import, is thread-safe under the
import lock, and is far easier to read. Use `@functools.cache` for a lazy one.

```python
@functools.cache
def get_pool() -> Pool:
    return Pool(dsn=settings.dsn)
```

Neither form is testable if callers reach for it directly — pass the instance in
as an argument at the composition root. The metaclass buys you only the
"`Pool()` must return the same object" guarantee; if nobody relies on that, drop it.

**Repo:** `src/Singleton/` (`NonThreadSafe/`, `ThreadSafe/`)
