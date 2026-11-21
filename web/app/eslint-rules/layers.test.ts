import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { ESLint } from "eslint";
import config from "../eslint.config.js";

const eslint = new ESLint({ overrideConfigFile: true, overrideConfig: config as never });
const at = (rel: string) => path.join(process.cwd(), "src", rel);

async function refused(file: string, code: string) {
  const [result] = await eslint.lintText(code, { filePath: at(file) });
  return (result?.messages ?? []).filter((m) => m.ruleId === "boundaries/layers").map((m) => m.message);
}

const REFUSED: [string, string, string][] = [
  ["domain imports application", "features/navigation/domain/a.ts", 'import "../application/b";'],
  ["domain imports infrastructure", "features/navigation/domain/a.ts", 'import "../infrastructure/b";'],
  ["domain imports presentation", "features/navigation/domain/a.ts", 'import "../presentation/views/b";'],
  ["domain imports React", "features/navigation/domain/a.ts", 'import React from "react";'],
  ["domain imports shared", "features/navigation/domain/a.ts", 'import "../../../shared/kernel/clock";'],
  ["application imports infrastructure", "features/navigation/application/a.ts", 'import "../infrastructure/b";'],
  ["application imports shared", "features/navigation/application/a.ts", 'import "../../../shared/kernel/clock";'],
  ["presentation imports infrastructure", "features/navigation/presentation/presenters/a.ts", 'import "../../infrastructure/b";'],
  ["presentation imports a shared adapter", "features/navigation/presentation/views/a.tsx", 'import "../../../../shared/infrastructure/api-client";'],
  ["a presenter imports React", "features/navigation/presentation/presenters/a.ts", 'import "react";'],
  ["a presenter imports a view", "features/navigation/presentation/presenters/a.ts", 'import "../views/b";'],
  ["a feature imports another feature", "features/navigation/application/a.ts", 'import "../../notifications/domain/b";'],
  ["a feature imports another feature's adapter", "features/navigation/infrastructure/a.ts", 'import "../../notifications/infrastructure/b";'],
  ["shared imports a feature", "shared/kernel/a.ts", 'import "../../features/navigation/domain/b";'],
  ["shared kernel imports React", "shared/kernel/a.ts", 'import "react";'],
  ["a feature imports a composition root", "features/navigation/infrastructure/a.ts", 'import "../../../composition/b";'],
];

for (const [name, file, code] of REFUSED) {
  test(`lint refuses: ${name}`, async () => {
    assert.equal((await refused(file, code)).length, 1);
  });
}

const ALLOWED: [string, string, string][] = [
  ["application imports its domain", "features/navigation/application/a.ts", 'import "../domain/b";'],
  ["infrastructure imports application, domain and shared", "features/navigation/infrastructure/a.ts", 'import "../application/b"; import "../domain/c"; import "../../../shared/infrastructure/api-client";'],
  ["a view imports React and its presenter", "features/navigation/presentation/views/a.tsx", 'import "react"; import "../presenters/b";'],
  ["a presenter imports the shared kernel", "features/navigation/presentation/presenters/a.ts", 'import "../../../../shared/kernel/presenter";'],
  ["a composition root imports an adapter", "composition/a.ts", 'import "../features/navigation/infrastructure/b";'],
  ["a test imports an adapter", "features/navigation/presentation/presenters/a.test.ts", 'import "../../infrastructure/b";'],
];

for (const [name, file, code] of ALLOWED) {
  test(`lint allows: ${name}`, async () => {
    assert.deepEqual(await refused(file, code), []);
  });
}

test("lint refuses a browser global on the server render's path, and allows it in a view", async () => {
  const code = "export const here = window.location.href;\n";
  const globals = async (file: string) => ((await eslint.lintText(code, { filePath: at(file) }))[0]?.messages ?? []).filter((m) => m.ruleId === "no-restricted-globals").length;
  assert.equal(await globals("islands/top-bar.server.ts"), 1);
  assert.equal(await globals("features/navigation/domain/a.ts"), 1);
  assert.equal(await globals("features/navigation/presentation/presenters/a.ts"), 1);
  assert.equal(await globals("features/navigation/presentation/views/a.ts"), 0);
});
