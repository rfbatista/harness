import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { formatMocks, parseMockTools } from "./case-text";

describe("the mock tools a person writes, one per line", () => {
  test("name = result is a canned result, name ! result an error result", () => {
    const { mocks, errors } = parseMockTools("read_file = # Demo project\nwrite_file ! permission denied");
    assert.deepEqual(errors, []);
    assert.deepEqual(mocks, [
      { name: "read_file", description: "", result: "# Demo project", isError: false },
      { name: "write_file", description: "", result: "permission denied", isError: true },
    ]);
  });

  test("a line with no name or no separator is refused with its line number", () => {
    const { errors } = parseMockTools("read_file = ok\njust words\n= nothing");
    assert.deepEqual(errors.map((e) => e.line), [2, 3]);
  });

  test("what is formatted parses back, keeping descriptions out of the text form", () => {
    const mocks = [{ name: "a", description: "", result: "x = y", isError: false }, { name: "b", description: "", result: "no", isError: true }];
    assert.deepEqual(parseMockTools(formatMocks(mocks)).mocks, mocks);
  });
});
