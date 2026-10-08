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
