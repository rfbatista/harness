# Structural Patterns

They decide **how objects are wired together** so composition, not inheritance,
carries the variation.

Repo examples: `~/dotfiles/skills/design-patterns-python/src/<Pattern>/Conceptual/main.py`

---

## Adapter

**Intent:** translate one interface into the one your code already expects.

**Reach for it when**
- A vendor SDK, legacy module, or third-party client has the wrong shape
- You cannot change either side
- Two libraries must be swappable behind one call site

**Shape (object adapter — prefer this)**

```python
class Target(Protocol):
    def request(self) -> str: ...

class Adapter:                       # composition, not inheritance
    def __init__(self, adaptee: Adaptee) -> None:
        self._adaptee = adaptee

    def request(self) -> str:
        return self._adaptee.specific_request()[::-1]
```

**Skip it when** one function converts the call. `def request(a: Adaptee) -> str`
is an adapter; a class adds nothing unless there is state or several methods.
Avoid the class-adapter form (multiple inheritance from both sides) — it inherits
the adaptee's whole surface, including what you meant to hide.

**Repo:** `src/Adapter/` (`object/` and `class/`)

---

## Bridge

**Intent:** split an abstraction from its implementation so both extend independently.

**Reach for it when**
- Subclasses multiply as `Kind × Platform` (`RemoteTV`, `RemoteRadio`, `AdvancedRemoteTV`, …)
- Two axes vary and neither owns the other
- The implementation must be swapped at runtime

**Shape**

```python
class Device(Protocol):                # implementation axis
    def enable(self) -> None: ...
    def set_volume(self, pct: int) -> None: ...

class Remote:                          # abstraction axis
    def __init__(self, device: Device) -> None:
        self._device = device

    def toggle_power(self) -> None: ...

class AdvancedRemote(Remote):          # extends one axis only
    def mute(self) -> None:
        self._device.set_volume(0)
```

**Skip it when** one axis has a single member. Bridge is Strategy grown up: use
Bridge when *both* sides are open hierarchies, Strategy when only the algorithm varies.

**Repo:** `src/Bridge/`

---

## Composite

**Intent:** let clients treat a single object and a tree of objects identically.

**Reach for it when**
- The domain is genuinely recursive: files/folders, groups, expressions, UI nodes
- Client code branches on `isinstance(x, Group)` before every operation

**Shape**

```python
class Component(ABC):
    @abstractmethod
    def total(self) -> float: ...

class Leaf(Component):
    def total(self) -> float:
        return self.price

class Composite(Component):
    def __init__(self) -> None:
        self._children: list[Component] = []

    def add(self, c: Component) -> None:
        self._children.append(c)

    def total(self) -> float:
        return sum(c.total() for c in self._children)   # recursion lives here
```

**Skip it when** the structure is one level deep — that is a list, not a Composite.

**Repo:** `src/Composite/`

---

## Decorator

**Intent:** add responsibilities to an object at runtime by wrapping it, stackably,
in any order, keeping the same interface.

**Reach for it when**
- Optional behaviors combine: compress, encrypt, cache, retry, audit
- The combinations would otherwise be a subclass per combination, or a hand-rolled
  "ordered list of steps" pipeline
- The order of the layers must be decided per call at runtime

**Shape**

```python
class Exporter(Protocol):
    def export(self, report: dict) -> bytes: ...

class ExporterDecorator:
    def __init__(self, wrapped: Exporter) -> None:
        self._wrapped = wrapped

class Gzipped(ExporterDecorator):
    def export(self, report: dict) -> bytes:
        return gzip.compress(self._wrapped.export(report))

class Encrypted(ExporterDecorator):
    def __init__(self, wrapped: Exporter, key: bytes) -> None:
        super().__init__(wrapped)
        self._key = key

    def export(self, report: dict) -> bytes:
        return Fernet(self._key).encrypt(self._wrapped.export(report))

exporter = Encrypted(Gzipped(PdfExporter()), key)   # order is the composition
```

Each layer carries only its own dependencies — `Gzipped` never sees the key. A flat
step table forces a uniform signature and leaks every parameter into every step.

**Skip it when** the wrapped thing is a single function. Then the decorator is a
function decorator or a fold:

```python
def compose(base: Callable[[], bytes], *layers) -> Callable[[], bytes]:
    return functools.reduce(lambda f, layer: layer(f), layers, base)
```

`@functools.wraps` decorators are this pattern; use them for cross-cutting
concerns on functions, the class form when the thing has state or many methods.

**Repo:** `src/Decorator/`

---

## Facade

**Intent:** put one small interface in front of a large subsystem.

**Reach for it when**
- A workflow needs 12 classes wired in a fixed order
- Callers copy the same 20-line setup
- You want one place to swap the subsystem later

**Shape**

```python
class VideoConverter:                       # the facade
    def convert(self, filename: str, fmt: str) -> File:
        file = VideoFile(filename)
        codec = CodecFactory.extract(file)
        buffer = BitrateReader.read(filename, codec)
        return (AudioMixer().fix(buffer))
```

A module of functions is a perfectly good facade in Python — the pattern is the
narrow public surface, not the class.

**Skip it when** the subsystem is already two calls, or the facade would only
forward one method (that is a needless layer).

**Repo:** `src/Facade/`

---

## Flyweight

**Intent:** share the invariant part of state across many objects to fit in memory.

**Reach for it when**
- Millions of objects exist and profiling shows duplicated fields dominate
- The duplicated part is genuinely immutable (intrinsic), and the rest can be passed in (extrinsic)

**Shape**

```python
@functools.lru_cache(maxsize=None)
def tree_type(name: str, color: str, texture: bytes) -> TreeType:
    return TreeType(name, color, texture)          # shared intrinsic state

class Tree:                                        # extrinsic state per instance
    __slots__ = ("x", "y", "type")
```

**Skip it until you have measured.** This is the one pattern justified by a
profiler, not by a change axis. In Python, `__slots__`, `sys.intern`, and
`@dataclass(frozen=True, slots=True)` often recover the memory with no pattern at
all. Flyweights must be immutable — a mutable shared flyweight corrupts every holder.

**Repo:** `src/Flyweight/`

---

## Proxy

**Intent:** stand in for another object, with the same interface, to control access to it.

**Reach for it when**
- Lazy/expensive initialization, caching, access control, logging, remote calls, rate limiting
- The real object's interface must not change and callers must not know

**Shape**

```python
class CachingProxy:
    def __init__(self, real: Service) -> None:
        self._real = real
        self._cache: dict[str, bytes] = {}

    def fetch(self, key: str) -> bytes:
        if key not in self._cache:
            self._cache[key] = self._real.fetch(key)
        return self._cache[key]
```

Python gives you sharper tools for the common cases: `functools.cached_property`
for lazy fields, `@functools.cache` for memoization, `__getattr__` to forward
everything you did not override.

**Skip it when** `@functools.cache` or `cached_property` already does it — say so,
and use them.

**Repo:** `src/Proxy/`
