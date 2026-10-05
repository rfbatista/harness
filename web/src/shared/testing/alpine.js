// Runs an Alpine.data() component without Alpine: the factory's object plus
// the magics it uses. Tests drive methods and read getters directly.

/**
 * @param {() => object} component  the inner factory registered with Alpine.data
 * @param {{ el?: HTMLElement }} [options]
 */
export function mount(component, { el = document.createElement("div") } = {}) {
  const dispatched = [];
  const watchers = new Map();
  const ticks = [];

  const instance = component();
  Object.defineProperties(instance, {
    $el: { value: el },
    $root: { value: el },
    // Empty unless a test sets its own (configurable, so it can).
    $refs: { value: {}, configurable: true, writable: true },
    $dispatch: {
      value: (name, detail) => dispatched.push({ name, detail }),
    },
    $nextTick: {
      value: (fn) => ticks.push(fn),
    },
    $watch: {
      value: (key, fn) => watchers.set(key, [...(watchers.get(key) ?? []), fn]),
    },
  });

  return {
    instance,
    /** Events the component dispatched, oldest first. */
    dispatched,
    /** Runs $nextTick callbacks, as Alpine does after rendering. */
    tick() {
      for (const fn of ticks.splice(0)) fn();
    },
    /** Sets a watched property and runs its watchers, as Alpine would. */
    set(key, value) {
      const old = instance[key];
      instance[key] = value;
      for (const fn of watchers.get(key) ?? []) fn(value, old);
    },
  };
}

/** A detached element with data-seed pointing at a JSON script holding `seed`. */
export function seededElement(seed, id = "test-seed") {
  const doc = document.implementation.createHTMLDocument("test");
  const script = doc.createElement("script");
  script.type = "application/json";
  script.id = id;
  script.textContent = JSON.stringify(seed);
  doc.body.append(script);
  const el = doc.createElement("main");
  el.dataset.seed = id;
  doc.body.append(el);
  return el;
}
