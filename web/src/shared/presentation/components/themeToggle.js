// Light / dark / system. Pins <html data-theme> (see design-system tokens)
// and remembers the choice per browser.
//
//   <div x-data="themeToggle">
//     <button class="[ button ]" @click="light" :aria-pressed="isLight">Light</button>

const KEY = "theme";

/** @param {import("../../infrastructure/storage.js").Preferences} prefs */
export const themeToggle = (prefs) => () => ({
  theme: "system",

  init() {
    const saved = prefs.get(KEY);
    this.apply(saved === "light" || saved === "dark" ? saved : "system");
  },

  get isLight() {
    return this.theme === "light";
  },
  get isDark() {
    return this.theme === "dark";
  },
  get isSystem() {
    return this.theme === "system";
  },

  light() {
    this.choose("light");
  },
  dark() {
    this.choose("dark");
  },
  system() {
    this.choose("system");
  },

  choose(theme) {
    this.apply(theme);
    prefs.set(KEY, theme === "system" ? null : theme);
  },

  apply(theme) {
    this.theme = theme;
    const root = this.$el.ownerDocument.documentElement;
    if (theme === "system") delete root.dataset.theme;
    else root.dataset.theme = theme;
  },
});
