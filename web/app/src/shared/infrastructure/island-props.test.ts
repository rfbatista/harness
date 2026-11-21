import { test } from "node:test";
import assert from "node:assert/strict";
import { IslandPropsError, readIslandProps } from "./island-props";

const root = (text: string | null) => ({
  querySelector: (selector: string) => (text !== null && selector === 'script[data-island-props="top-bar"]' ? { textContent: text } : null),
}) as unknown as ParentNode;

test("reads the props JSON of the named island", () => {
  assert.deepEqual(readIslandProps(root('{"path":"/"}'), "top-bar"), { path: "/" });
});

test("a missing script is an IslandPropsError", () => {
  assert.throws(() => readIslandProps(root(null), "top-bar"), IslandPropsError);
});

test("malformed JSON is an IslandPropsError", () => {
  assert.throws(() => readIslandProps(root("{"), "top-bar"), IslandPropsError);
});
