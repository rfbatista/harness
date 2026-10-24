// The writing helper module's Alpine components: the magic pen and its panel.

import { createCoordinator } from "./coordinator.js";
import { writingPen } from "./components/writingPen.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: import("../domain/ports.js").StepGateway }} deps
 */
export function registerWritingHelper(Alpine, deps) {
  Alpine.data("writingPen", writingPen({ ...deps, coordinator: createCoordinator() }));
}
