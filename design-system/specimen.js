// Runs the scripted dropdowns on the specimen. Serve from the repository root
// (python3 -m http.server) so ../web resolves; opened as a file, the page still
// shows every block, with the scripted variants as their native selects.

import Alpine from "../web/vendor/alpine-csp.esm.js";
import { combobox } from "../web/src/shared/presentation/components/combobox.js";
import { dropdown } from "../web/src/shared/presentation/components/dropdown.js";
import { menu } from "../web/src/shared/presentation/components/menu.js";

const TASKS = ["Add SSE feed", "Dropdown component", "Terminal keyboard focus", "Project management", "Pull requests", "Design assets attached to tasks"];

Alpine.data("combobox", combobox());
Alpine.data("dropdown", dropdown());
Alpine.data("menu", menu());
// A query source for the Combobox: a feature's method, here a fake search.
Alpine.data("specimenSearch", () => ({
  searchTasks(query, signal) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        if (query.toLowerCase().includes("fail")) reject(new Error("search failed"));
        else resolve(TASKS.filter((t) => t.toLowerCase().includes(query.toLowerCase())).map((t, i) => ({ value: `t${i}`, label: t, meta: "in progress" })));
      }, 400);
      signal.addEventListener("abort", () => clearTimeout(timer));
    });
  },
}));
Alpine.start();

// Phone frames: the specimen at 375px with one sheet open in each. Inside a
// frame (?open=<id>) the page opens that control; outside, it draws the frames.
const params = new URLSearchParams(location.search);
const opening = params.get("open");
if (opening) {
  setTimeout(() => {
    const el = document.getElementById(opening);
    el?.scrollIntoView({ block: "center" });
    if (el instanceof HTMLInputElement) {
      el.focus();
      el.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    } else {
      el?.click();
    }
  }, 200);
} else {
  const frames = [
    ["l-agent-trigger", "Listbox select"],
    ["c-labels-input", "Combobox, several values"],
    ["m-session", "Menu button"],
  ];
  document.querySelector("[data-phone-frames]")?.append(
    ...frames.map(([id, title]) => {
      const frame = document.createElement("iframe");
      frame.src = `${location.pathname}?open=${id}`;
      frame.title = `${title} as a bottom sheet, at 375px`;
      frame.width = "375";
      frame.height = "667";
      frame.loading = "lazy";
      frame.style.border = "var(--border-width) solid var(--color-line)";
      frame.style.borderRadius = "var(--radius-md)";
      return frame;
    }),
  );
}
