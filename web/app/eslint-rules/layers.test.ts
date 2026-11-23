import { describe, test } from "node:test";
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
  ["a feature imports a file of web/src (the Alpine client)", "features/navigation/infrastructure/a.ts", 'import "../../../../../src/shared/infrastructure/api.js";'],
  ["a view imports a file of web/src", "features/notifications/presentation/views/a.tsx", 'import "../../../../../../src/shared/presentation/components/popup.js";'],
  ["shared kernel imports a file of web/src", "shared/kernel/a.ts", 'import "../../../../src/shared/domain/feed.js";'],
  ["another shared file than use-popup imports popup.js", "shared/presentation/other.ts", 'import "../../../../src/shared/presentation/components/popup.js";'],
  ["use-popup imports another file of web/src", "shared/presentation/use-popup.ts", 'import "../../../../src/shared/infrastructure/api.js";'],
  ["a domain file imports a DTO", "features/navigation/domain/a.ts", 'import "../infrastructure/dto/top-bar-dto";'],
  ["an application file imports a DTO", "features/navigation/application/a.ts", 'import "../infrastructure/dto/top-bar-dto";'],
  ["a presenter imports a DTO", "features/notifications/presentation/presenters/a.ts", 'import "../../infrastructure/dto/notification-dto";'],
  ["a view imports a DTO", "features/notifications/presentation/views/a.tsx", 'import "../../infrastructure/dto/notification-dto";'],
  ["shared presentation imports a shared DTO", "shared/presentation/other.ts", 'import "../infrastructure/dto/dto-error";'],
  ["shared kernel imports a shared DTO", "shared/kernel/a.ts", 'import "../infrastructure/dto/dto-error";'],
  ["a feature's infrastructure imports another feature's DTO", "features/navigation/infrastructure/a.ts", 'import "../../notifications/infrastructure/dto/notification-dto";'],
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
  ["use-popup imports the Alpine-free popup module (the named exception)", "shared/presentation/use-popup.ts", 'import "../../../../src/shared/presentation/components/popup.js";'],
  ["a composition root imports web/src", "composition/a.ts", 'import "../../../src/shared/domain/feed.js";'],
  ["a testing folder imports web/src", "shared/testing/a.ts", 'import "../../../../src/shared/testing/doubles.js";'],
  ["a feature's adapter imports its own DTO", "features/navigation/infrastructure/a.ts", 'import "./dto/top-bar-dto";'],
  ["a feature's adapter imports a shared DTO", "features/navigation/infrastructure/a.ts", 'import "../../../shared/infrastructure/dto/dto-error";'],
  ["a shared adapter imports a shared DTO", "shared/infrastructure/a.ts", 'import "./dto/dto-error";'],
  ["a composition root imports a DTO", "composition/a.ts", 'import "../features/navigation/infrastructure/dto/top-bar-dto";'],
  ["a composition root imports an adapter", "composition/a.ts", 'import "../features/navigation/infrastructure/b";'],
  ["a test imports an adapter", "features/navigation/presentation/presenters/a.test.ts", 'import "../../infrastructure/b";'],
];

for (const [name, file, code] of ALLOWED) {
  test(`lint allows: ${name}`, async () => {
    assert.deepEqual(await refused(file, code), []);
  });
}

test("lint refuses a browser global on the server render's path, and in the views and shared/presentation", async () => {
  const code = "export const here = window.location.href;\n";
  const globals = async (file: string) => ((await eslint.lintText(code, { filePath: at(file) }))[0]?.messages ?? []).filter((m) => m.ruleId === "no-restricted-globals").length;
  assert.equal(await globals("islands/top-bar.server.ts"), 1);
  assert.equal(await globals("features/navigation/domain/a.ts"), 1);
  assert.equal(await globals("features/navigation/presentation/presenters/a.ts"), 1);
  assert.equal(await globals("features/navigation/presentation/views/a.ts"), 1);
  assert.equal(await globals("shared/presentation/a.ts"), 1);
});

// ADR 0106: outside the DTOs and the transport, no raw payload is touched.
describe("raw data rules", () => {
  const raw = async (file: string, code: string) => ((await eslint.lintText(code, { filePath: at(file) }))[0]?.messages ?? []).filter((m) => ["no-restricted-globals", "no-restricted-properties", "no-restricted-syntax"].includes(m.ruleId ?? "")).length;

  const REFUSED_CALLS: [string, string, string][] = [
    ["a presenter calls JSON.parse", "features/navigation/presentation/presenters/a.ts", "export const a = JSON.parse('{}');"],
    ["a use case calls fetch", "features/navigation/application/a.ts", "export const a = () => fetch('/api/x');"],
    ["a view reads event.detail", "features/notifications/presentation/views/a.tsx", "export const a = (event: CustomEvent) => event.detail;"],
    ["a handler reads event.data", "features/notifications/infrastructure/a.ts", "export const a = (event: MessageEvent) => event.data;"],
    ["a handler destructures data from its event", "features/notifications/infrastructure/a.ts", "export const a = ({ data }: MessageEvent) => data;"],
    ["an adapter outside the transport calls postMessage", "features/notifications/infrastructure/a.ts", "export const a = (c: BroadcastChannel) => c.postMessage({});"],
    ["an adapter outside the transport calls dispatchEvent", "features/navigation/infrastructure/a.ts", "export const a = (w: Window) => w.dispatchEvent(new Event('x'));"],
    ["an adapter calls JSON.stringify", "features/notifications/infrastructure/a.ts", "export const a = JSON.stringify({});"],
    ["a view uses localStorage", "features/notifications/presentation/views/a.tsx", "export const a = localStorage.getItem('x');"],
    ["a root uses sessionStorage", "composition/a.ts", "export const a = sessionStorage.getItem('x');"],
    ["a use case calls globalThis.fetch", "features/navigation/application/a.ts", "export const a = () => globalThis.fetch('/api/x');"],
    ["an adapter calls self.fetch", "features/navigation/infrastructure/a.ts", "export const a = () => self.fetch('/api/x');"],
    ["an adapter calls window.fetch", "features/navigation/infrastructure/a.ts", "export const a = () => window.fetch('/api/x');"],
    ["an adapter reads window.localStorage", "features/notifications/infrastructure/a.ts", "export const a = window.localStorage.getItem('x');"],
    ["an adapter reads sessionStorage through globalThis", "features/notifications/infrastructure/a.ts", "export const a = globalThis.sessionStorage.length;"],
    ["a view reads document.cookie", "features/notifications/presentation/views/a.tsx", "export const a = document.cookie;"],
    ["an adapter uses indexedDB", "features/notifications/infrastructure/a.ts", "export const a = indexedDB.open('x');"],
    ["an adapter reads storage by a computed key", "features/notifications/infrastructure/a.ts", "export const a = (w: Window) => w['localStorage'];"],
    ["an adapter reads .data of any receiver", "features/notifications/infrastructure/a.ts", "export const a = (m: { data: unknown }) => m.data;"],
    ["an adapter reads .detail of any receiver", "features/navigation/infrastructure/a.ts", "export const a = (x: { detail: unknown }) => x.detail;"],
    ["an adapter reads .data by a computed key", "features/navigation/infrastructure/a.ts", "export const a = (m: { data: unknown }) => m['data'];"],
    ["an adapter reads .data through a cast", "features/navigation/infrastructure/a.ts", "export const a = (m: unknown) => (m as MessageEvent).data;"],
    ["a presenter destructures data in a parameter", "features/notifications/presentation/presenters/a.ts", "export function a({ data }: { data: unknown }) { return data; }"],
    ["a presenter destructures detail in a declaration", "features/notifications/presentation/presenters/a.ts", "export const a = (e: unknown) => { const { detail } = e as CustomEvent; return detail; };"],
    ["a use case destructures a renamed data", "features/navigation/application/a.ts", "export const a = (e: MessageEvent) => { const { data: raw } = e; return raw; };"],
  ];
  for (const [name, file, code] of REFUSED_CALLS) {
    test(`lint refuses: ${name}`, async () => {
      assert.ok((await raw(file, code)) >= 1);
    });
  }

  const SERVER_GLOBALS: [string, string][] = [
    ["window", "export const a = window.innerWidth;"],
    ["document", "export const a = document.title;"],
    ["process", "export const a = process.env;"],
    ["fetch", "export const a = () => fetch('/x');"],
    ["localStorage", "export const a = localStorage.length;"],
  ];
  for (const [name, code] of SERVER_GLOBALS) {
    test(`lint refuses a DTO that uses ${name}: DTOs run in the server render`, async () => {
      assert.ok((await raw("features/navigation/infrastructure/dto/a.ts", code)) >= 1);
      assert.ok((await raw("shared/infrastructure/dto/a.ts", code)) >= 1);
    });
  }

  const ALLOWED_CALLS: [string, string, string][] = [
    ["the API client calls fetch", "shared/infrastructure/api-client.ts", "export const a = () => fetch('/api/x');"],
    ["the stream feed parses JSON", "shared/infrastructure/stream-feed.ts", "export const a = JSON.parse('{}');"],
    ["the shared feed reads a channel message and posts one", "shared/infrastructure/shared-feed.ts", "export const a = ({ data }: MessageEvent, c: BroadcastChannel) => c.postMessage(data);"],
    ["the browser navigation signal reads event.detail", "features/navigation/infrastructure/browser-navigation-signal.ts", "export const a = (event: CustomEvent) => event.detail;"],
    ["a DTO calls JSON.stringify", "features/notifications/infrastructure/dto/a.ts", "export const a = JSON.stringify({});"],
    ["a test builds raw data", "features/notifications/infrastructure/a.test.ts", "export const a = JSON.parse('{}');"],
    ["a testing helper dispatches an event", "features/navigation/testing/a.ts", "export const a = (w: Window) => w.dispatchEvent(new Event('x'));"],
    ["the shared feed reads .data and destructures it", "shared/infrastructure/shared-feed.ts", "export const a = (m: MessageEvent) => { const { data } = m; return [data, m.data, (m as MessageEvent).data]; };"],
    ["a DTO reads .data and .detail of its own", "features/navigation/infrastructure/dto/a.ts", "export const a = (m: { data: unknown; detail: unknown }) => [m.data, m.detail];"],
    ["a view reads an event's other fields", "features/notifications/presentation/views/a.tsx", "export const a = (event: KeyboardEvent) => event.key;"],
  ];
  for (const [name, file, code] of ALLOWED_CALLS) {
    test(`lint allows: ${name}`, async () => {
      assert.equal(await raw(file, code), 0);
    });
  }
});
