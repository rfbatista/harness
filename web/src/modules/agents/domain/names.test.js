import { assert, file, test } from "../../../shared/testing/test.js";
import { isMcpName, isSlug, mcpNameProblem, placeholderNames, placeholderProblem, isPlaceholder, slugProblem } from "./names.js";

file("agents/domain/names");

test("a slug is lowercase letters, digits and hyphens, from 1 to 64 characters, not starting with a hyphen", () => {
  for (const ok of ["tdd", "ux-designer", "a", "0day", "a".repeat(64), "spec-implementer-2"]) {
    assert.ok(isSlug(ok), `${ok} is a slug`);
  }
  for (const bad of ["", "-tdd", "TDD", "ux designer", "ux_designer", "a".repeat(65), "@ux", "tdd/", "é", " tdd", "tdd\n"]) {
    assert.ok(!isSlug(bad), `${JSON.stringify(bad)} is not a slug`);
  }
  assert.ok(!isSlug(undefined));
});

test("an MCP server name allows letters of both cases, digits, underscore and hyphen, up to 64", () => {
  for (const ok of ["github", "Linear_API", "my-server", "_x", "-x", "A".repeat(64)]) assert.ok(isMcpName(ok), `${ok} is a name`);
  for (const bad of ["", "my server", "a.b", "a".repeat(65), "é", "x\n"]) assert.ok(!isMcpName(bad), `${JSON.stringify(bad)} is not a name`);
});

test("slugProblem says what is wrong, in the person's words, and nothing for a good name", () => {
  assert.equal(slugProblem("tdd", "Skill name"), "");
  assert.equal(slugProblem("", "Skill name"), "Skill name is required.");
  assert.equal(slugProblem("a".repeat(65), "Skill name"), "Skill name is at most 64 characters.");
  assert.equal(slugProblem("TDD", "Skill name"), "Skill name uses lowercase letters, digits and hyphens, and starts with a letter or digit.");
  assert.equal(slugProblem("-tdd", "Bundle name"), "Bundle name uses lowercase letters, digits and hyphens, and starts with a letter or digit.");
});

test("mcpNameProblem names the characters an MCP server name allows", () => {
  assert.equal(mcpNameProblem("github"), "");
  assert.equal(mcpNameProblem(""), "Server name is required.");
  assert.equal(mcpNameProblem("a".repeat(65)), "Server name is at most 64 characters.");
  assert.equal(mcpNameProblem("my server"), "Server name uses letters, digits, underscores and hyphens.");
});

test("placeholderNames lists each ${VAR} once, in order of appearance", () => {
  assert.deepEqual(placeholderNames("Bearer ${LINEAR_TOKEN}"), ["LINEAR_TOKEN"]);
  assert.deepEqual(placeholderNames("${A}/${B}/${A}"), ["A", "B"]);
  assert.deepEqual(placeholderNames("plain"), []);
  assert.deepEqual(placeholderNames("$A and {B} and ${1X}"), []);
  assert.deepEqual(placeholderNames(undefined), []);
});

test("isPlaceholder accepts a value that is exactly one ${VAR}", () => {
  assert.ok(isPlaceholder("${GITHUB_TOKEN}"));
  assert.ok(isPlaceholder("${_x1}"));
  assert.ok(!isPlaceholder("Bearer ${T}"));
  assert.ok(!isPlaceholder("${}"));
  assert.ok(!isPlaceholder("${1A}"));
  assert.ok(!isPlaceholder("$GITHUB_TOKEN"));
  assert.ok(!isPlaceholder("${A}${B}"));
});

test("placeholderProblem rejects a reference that is empty, badly named or never closed", () => {
  assert.equal(placeholderProblem("Bearer ${LINEAR_TOKEN}"), "");
  assert.equal(placeholderProblem("no references"), "");
  assert.equal(placeholderProblem("${}"), "${} names no variable.");
  assert.equal(placeholderProblem("${1ABC}"), "${1ABC} is not a variable name: use letters, digits and underscores, not starting with a digit.");
  assert.equal(placeholderProblem("x ${OPEN"), "${OPEN is missing its closing brace.");
});
