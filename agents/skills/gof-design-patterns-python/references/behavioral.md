# Behavioral Patterns

They decide **how responsibility and control flow move at runtime** between objects.

Repo examples: `~/dotfiles/skills/design-patterns-python/src/<Pattern>/Conceptual/main.py`

---

## Chain of Responsibility

**Intent:** pass a request along an ordered chain until one handler deals with it.

**Reach for it when**
- Validation/auth/middleware steps are optional, ordered, and reorderable
- Any step may stop the chain (short-circuit)
- The set of steps is configured, not hard-coded

**Shape**

```python
class Handler(ABC):
    def __init__(self) -> None:
        self._next: Handler | None = None

    def set_next(self, h: "Handler") -> "Handler":
        self._next = h
        return h

    def handle(self, request: Request) -> Response | None:
        return self._next.handle(request) if self._next else None

class AuthHandler(Handler):
    def handle(self, request: Request) -> Response | None:
        if not request.user:
            return Response(401)          # stops the chain
        return super().handle(request)
```

**Skip it when** the steps are functions and never reorder — a list of
`Callable[[Request], Response | None]` walked in a loop is the same pattern with
one tenth the code, and is what WSGI/ASGI middleware already is.

**Repo:** `src/ChainOfResponsibility/`

---

## Command

**Intent:** reify a call — receiver, method, and arguments — as an object, so it can
be stored, queued, logged, replayed, and undone.

**Reach for it when**
- **Undo/redo is required** (the giveaway)
- Actions must be queued, retried, scheduled, or written to an audit trail
- The invoker (button, CLI, worker) must not know what the action does

**Shape**

```python
class Command(ABC):
    @abstractmethod
    def execute(self) -> None: ...
    @abstractmethod
    def undo(self) -> None: ...

class ShipOrder(Command):
    def __init__(self, order: Order) -> None:
        self._order = order
        self._previous: OrderState | None = None

    def execute(self) -> None:
        self._previous = self._order.state       # capture for undo
        self._order.state = OrderState.SHIPPED

    def undo(self) -> None:
        self._order.state = self._previous

class History:                                   # the invoker owns the stack
    def __init__(self) -> None:
        self._done: list[Command] = []

    def run(self, cmd: Command) -> None:
        cmd.execute()
        self._done.append(cmd)

    def undo(self) -> None:
        if self._done:
            self._done.pop().undo()
```

Undo belongs to the command that did the thing, not to a history of raw values —
that is what lets undo reverse *side effects* (refund the charge, cancel the
label), not just restore a field.

**Skip it when** nothing is stored or reversed. A queued job with no undo is a
function plus arguments — use `functools.partial` or a `@dataclass` of args.

**Repo:** `src/Command/`

---

## Iterator

**Intent:** traverse a collection without exposing how it is stored.

**Reach for it when**
- Traversal order varies (depth-first vs breadth-first, forward vs reverse)
- Internals must stay hidden, or the sequence is lazy/infinite

**Shape**

```python
class Tree:
    def __iter__(self) -> Iterator[Node]:        # the Python protocol IS the pattern
        yield from self._walk(self._root)

    def breadth_first(self) -> Iterator[Node]:   # a second, named traversal
        ...
```

**Skip the GoF class form entirely.** Implement `__iter__` and use generators.
A hand-written class with `__next__` and an index is justified only when the
iterator carries resettable state clients must hold.

**Repo:** `src/Iterator/`

---

## Mediator

**Intent:** move N-to-N communication into one object the components talk to instead.

**Reach for it when**
- Every component holds references to every other one
- Cross-component rules ("checking this box disables that field") are smeared across components

**Shape**

```python
class Mediator(Protocol):
    def notify(self, sender: object, event: str) -> None: ...

class Dialog:
    def notify(self, sender: object, event: str) -> None:
        if sender is self.checkbox and event == "toggled":
            self.text_field.enabled = self.checkbox.checked
```

**Skip it when** the mediator becomes a god object that knows every component's
internals. At that point split it or use Observer — see the confusion table in
SKILL.md.

**Repo:** `src/Mediator/`

---

## Memento

**Intent:** capture and restore an object's state without exposing its internals.

**Reach for it when**
- Snapshot/restore, checkpoints, transactions, "revert to draft"
- The state to save is private and must stay private

**Shape**

```python
@dataclass(frozen=True)
class EditorMemento:                    # opaque to everyone but the Originator
    _content: str
    _cursor: int

class Editor:
    def save(self) -> EditorMemento:
        return EditorMemento(self._content, self._cursor)

    def restore(self, m: EditorMemento) -> None:
        self._content, self._cursor = m._content, m._cursor
```

**Pair it with Command** when you need undo of *state* rather than undo of an
action: the command holds the memento it took before executing.

**Skip it when** the state is one public field — save the value.

**Repo:** `src/Memento/`

---

## Observer

**Intent:** let many objects react to one object's events without it knowing them.

**Reach for it when**
- "…and also notify X" keeps arriving (audit, email, metrics, webhooks)
- The publisher must not import the subscribers

**Shape**

```python
class Observer(Protocol):
    def update(self, event: Event) -> None: ...

class Subject:
    def __init__(self) -> None:
        self._observers: list[Observer] = []

    def attach(self, o: Observer) -> None:
        self._observers.append(o)

    def _notify(self, event: Event) -> None:
        for o in list(self._observers):          # copy: handlers may detach
            o.update(event)
```

Notes: hold subscribers in a `weakref.WeakSet` if the subject outlives them;
decide explicitly whether one failing observer aborts the rest (it usually
should not); an observer list is not an event bus — do not let ordering become
load-bearing.

**Skip it when** there are exactly two fixed listeners forever, or the codebase
already has signals (Django `Signal`, `blinker`) — use those; they are this pattern.

**Repo:** `src/Observer/`

---

## State

**Intent:** let an object change its behavior when its internal state changes, by
delegating to a state object; illegal transitions become impossible rather than checked.

**Reach for it when**
- Methods start with `if self.status == ...` in several places
- Each state permits a *different set of operations*
- Transitions are constrained and drift between parallel tables would be a bug

**Shape**

```python
class OrderState(ABC):
    @abstractmethod
    def pay(self, order: "Order") -> None: ...   # unimplemented in states that forbid it

class Draft(OrderState):
    def submit(self, order: "Order") -> None:
        order.transition_to(Submitted())
    def pay(self, order: "Order") -> None:
        raise IllegalTransition("draft cannot be paid")

class Order:
    def __init__(self) -> None:
        self._state: OrderState = Draft()

    def transition_to(self, state: OrderState) -> None:
        self._state = state

    def pay(self) -> None:
        self._state.pay(self)                    # no if/elif anywhere
```

**Skip it when** states carry no behavior, only permissions. Then one table is
right — but keep it to **one** table (`state -> (allowed_ops, next_states)`);
two dicts keyed by the same enum drift apart. Use `enum.Enum` for the states, and
a state class per state as soon as each state has its own logic.

**Repo:** `src/State/`

---

## Strategy

**Intent:** make interchangeable algorithms swappable at runtime behind one interface.

**Reach for it when**
- Several algorithms do the same job (sorting, pricing, routing, retry policy)
- The choice is made by the caller or by configuration

**Shape**

```python
class Strategy(Protocol):
    def __call__(self, data: list[int]) -> list[int]: ...

class Sorter:
    def __init__(self, strategy: Strategy) -> None:
        self._strategy = strategy
```

**Skip the class hierarchy.** In Python a strategy is a function: pass
`key=`, a `Callable`, or `functools.partial`. `sorted(xs, key=...)` is Strategy.
Use classes only when a strategy needs construction parameters *and* several methods.

Strategies must be stateless — per-call state is an argument, never an attribute.

**Repo:** `src/Strategy/`

---

## Template Method

**Intent:** fix an algorithm's skeleton in a base class and let subclasses fill in steps.

**Reach for it when**
- Several flows share order and differ only in a few steps
- Some steps are required, others are optional hooks

**Shape**

```python
class Importer(ABC):
    def run(self) -> None:            # the skeleton — do not override
        self.extract()
        self.transform()
        self.load()
        self.on_finished()            # optional hook, default no-op

    @abstractmethod
    def extract(self) -> None: ...
    def on_finished(self) -> None: ...
```

**Skip it when** the varying steps can be passed in — that is Strategy, and it
composes; Template Method locks you into inheritance. Prefer Strategy when a
subclass would override exactly one step.

**Repo:** `src/TemplateMethod/`

---

## Visitor

**Intent:** add new operations over a stable set of node types without touching them.

**Reach for it when**
- Node types are stable, operations keep being added (AST: type-check, format, compile)
- The operation's logic belongs together, not scattered across node classes

**Shape (Python collapse — prefer this)**

```python
@functools.singledispatch
def evaluate(node: Node) -> int:
    raise NotImplementedError(type(node))

@evaluate.register
def _(node: Literal) -> int:
    return node.value

@evaluate.register
def _(node: Add) -> int:
    return evaluate(node.left) + evaluate(node.right)
```

`functools.singledispatch` (or `match` with class patterns) gives you double
dispatch without the `accept(visitor)` boilerplate. Use the classic
`accept`/`visit_*` pair only when a visitor carries state across nodes and
traversal order matters.

**Skip it when** node types change more often than operations — every new node
type then breaks every visitor. That is Visitor's cost, and it is not negotiable.

**Repo:** `src/Visitor/`
