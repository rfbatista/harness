// The architecture module's Alpine components.

import { architecturePage } from "./pages/architecturePage.js";
import { instructionsTab } from "./tabs/instructionsTab.js";
import { toolsTab } from "./tabs/toolsTab.js";
import { sandboxesTab } from "./tabs/sandboxesTab.js";
import { orchestrationTab } from "./tabs/orchestrationTab.js";
import { hooksTab } from "./tabs/hooksTab.js";
import { layersTab } from "./tabs/layersTab.js";
import { outputTab } from "./tabs/outputTab.js";
import { outputDiagram } from "./tabs/outputDiagram.js";

/**
 * @param {import("alpinejs").Alpine} Alpine
 * @param {{ gateway: ReturnType<typeof import("../infrastructure/architecture-gateway.js").architectureGateway> }} deps
 */
export function registerArchitecture(Alpine, deps) {
  Alpine.data("architecturePage", architecturePage(deps));
  Alpine.data("architectureInstructionsTab", instructionsTab(deps));
  Alpine.data("architectureToolsTab", toolsTab(deps));
  Alpine.data("architectureSandboxesTab", sandboxesTab(deps));
  Alpine.data("architectureOrchestrationTab", orchestrationTab(deps));
  Alpine.data("architectureHooksTab", hooksTab(deps));
  Alpine.data("architectureLayersTab", layersTab(deps));
  Alpine.data("architectureOutputTab", outputTab(deps));
  Alpine.data("architectureOutputDiagram", outputDiagram(deps));
}
