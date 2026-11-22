// The import-boundary rules of ADR 0101, as one ESLint rule.
//
//   domain          imports only its own feature's domain, and no package
//   application     imports only its own feature's domain and application, and no package
//   infrastructure  imports its own feature's domain, application and infrastructure, and shared
//   presentation    imports its own feature's domain, application and presentation, and shared/kernel;
//                   presenters import no React and no view, never an adapter
//   shared/kernel   imports only shared/kernel, and no React
//   features        never import each other, only shared
//   composition roots (composition/, islands/, main.client.tsx) import anything: they alone build adapters
//   tests, and the testing/ folders (fixtures, port contract suites): import anything
//   nothing else imports from outside web/app/src, but use-popup.ts's one named exception

import path from "node:path";
import { fileURLToPath } from "node:url";

// web/app/src, wherever eslint runs from: only files under it have a place in the layers.
const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "src");
// The one file allowed to import from outside web/app/src (ADR 0101): the panel's popup reuses the Alpine-free
// popup module of web/src until the Alpine client goes.
const OUTSIDE_EXCEPTION = {
  importer: path.join(SRC, "shared", "presentation", "use-popup.ts"),
  target: path.resolve(SRC, "..", "..", "src", "shared", "presentation", "components", "popup.js"),
};
const FRAMEWORKS = /^react(-dom)?($|\/)/;

/** @param {string} file absolute path */
export function classify(file) {
  const relative = path.relative(SRC, file);
  if (relative.startsWith("..") || path.isAbsolute(relative)) return { zone: "elsewhere" };
  const rest = relative.split(path.sep);
  if (rest[0] === "features" && rest.length > 3) return { zone: "feature", feature: rest[1], layer: rest[2], sub: rest[3] };
  if (rest[0] === "shared" && rest.length > 2) return { zone: "shared", layer: rest[1] };
  if (rest[0] === "composition" || rest[0] === "islands" || rest[0]?.startsWith("main.")) return { zone: "root" };
  return { zone: "outside" };
}

const ALLOWED_LAYERS = {
  domain: ["domain"],
  application: ["domain", "application"],
  infrastructure: ["domain", "application", "infrastructure"],
  presentation: ["domain", "application", "presentation"],
};

const NO_SHARED = ["domain", "application"];

/** @returns {string | null} why the import is refused, or null */
export function refusal(importer, target, specifier, files = {}) {
  const bare = !specifier.startsWith(".");
  if (importer.zone === "root" || importer.zone === "outside" || importer.zone === "elsewhere") return null;
  if (importer.layer === "testing") return null;
  if (!bare && target.zone === "elsewhere") {
    const allowed = files.importer === OUTSIDE_EXCEPTION.importer && files.target === OUTSIDE_EXCEPTION.target;
    return allowed ? null : "nothing leaves web/app/src, but the composition roots and testing";
  }
  if (bare) {
    if (importer.zone === "shared" && importer.layer === "kernel" && FRAMEWORKS.test(specifier)) return "shared/kernel imports no React";
    if (importer.zone === "feature") {
      if (NO_SHARED.includes(importer.layer)) return `${importer.layer} imports no package`;
      if (importer.layer === "presentation" && importer.sub === "presenters" && FRAMEWORKS.test(specifier)) return "a presenter imports no React";
    }
    return null;
  }
  if (target.zone === "root") return "nothing imports a composition root";
  if (importer.zone === "shared") {
    if (target.zone !== "shared") return "shared imports no feature";
    if (importer.layer === "kernel" && target.layer !== "kernel") return "shared/kernel imports only shared/kernel";
    return null;
  }
  // a feature file
  if (target.zone === "feature") {
    if (target.feature !== importer.feature) return "features never import each other, only shared";
    if (!ALLOWED_LAYERS[importer.layer]?.includes(target.layer)) return `${importer.layer} may not import ${target.layer}`;
    if (importer.layer === "presentation" && importer.sub === "presenters" && target.layer === "presentation" && target.sub === "views") return "a presenter imports no view";
    return null;
  }
  if (target.zone === "shared") {
    if (NO_SHARED.includes(importer.layer)) return `${importer.layer} may not import shared`;
    if (target.layer === "infrastructure" && importer.layer !== "infrastructure") return `${importer.layer} may not import an adapter`;
    return null;
  }
  return null;
}

const isTest = (file) => /\.test\.tsx?$/.test(file);

/** @type {import("eslint").Rule.RuleModule} */
export const layers = {
  meta: { type: "problem", schema: [], messages: { refused: "{{why}}: {{specifier}}" } },
  create(context) {
    const file = context.filename;
    if (isTest(file)) return {};
    const importer = classify(file);
    const check = (node, source) => {
      if (!source || typeof source.value !== "string") return;
      const specifier = source.value;
      const resolved = specifier.startsWith(".") ? path.resolve(path.dirname(file), specifier) : "";
      const target = specifier.startsWith(".") ? classify(resolved) : { zone: "outside" };
      const why = refusal(importer, target, specifier, { importer: file, target: resolved });
      if (why) context.report({ node, messageId: "refused", data: { why, specifier } });
    };
    return {
      ImportDeclaration: (node) => check(node, node.source),
      ExportNamedDeclaration: (node) => check(node, node.source),
      ExportAllDeclaration: (node) => check(node, node.source),
      ImportExpression: (node) => check(node, node.source),
    };
  },
};

export default { rules: { layers } };
