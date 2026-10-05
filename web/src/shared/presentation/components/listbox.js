// A keyboard-driven list (J/K, ↑/↓, Home/End, Enter) over [role="option"]
// children. Emits `listbox-choose` with the chosen index.
//
//   <div x-data="listbox" role="listbox" tabindex="0"
//        @keydown.j.prevent="next" @keydown.k.prevent="previous"
//        @keydown.enter.prevent="choose">

export const listbox = () => () => ({
  active: 0,
  count: 0,

  init() {
    this.count = this.$el.querySelectorAll('[role="option"]').length;
  },

  move(delta) {
    this.active = Math.max(0, Math.min(this.count - 1, this.active + delta));
  },
  next() {
    this.move(1);
  },
  previous() {
    this.move(-1);
  },
  first() {
    this.active = 0;
  },
  last() {
    this.active = Math.max(0, this.count - 1);
  },
  choose() {
    this.$dispatch("listbox-choose", { index: this.active });
  },
});
