// The stream bar's keys, when a part of the page has its own: a component
// sends a `stream-hints` window event with [{ keys, label }] while it is in
// front, and null when it leaves; the page's own hints come back then.
//
//   <span class="[ keys ]" x-data="streamHints" x-on:stream-hints.window="set">
//     <span x-show="!overridden">…the page's hints…</span>
//     <template x-for="h in hints">…</template>
//   </span>

export const streamHints = () => () => ({
  /** @type {{ key: string, keys: string[], label: string }[]} */
  hints: [],

  get overridden() {
    return this.hints.length > 0;
  },

  set(event) {
    const list = Array.isArray(event.detail) ? event.detail : [];
    this.hints = list.map((h, i) => ({ key: `${i}:${h.label}`, keys: [...h.keys], label: h.label }));
  },
});
