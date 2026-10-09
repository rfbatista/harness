// Both RailPrefs implementations run the same contract; the cookie's own
// format is pinned here.

import { assert, file, test } from "../../../shared/testing/test.js";
import { railPrefsContract } from "../testing/rail-prefs-contract.js";
import { memoryRailPrefs } from "./memory-rail-prefs.js";
import { cookieRailPrefs } from "./rail-prefs.js";

file("tasks/infrastructure/rail-prefs");

/** A cookie jar as document.cookie behaves: set one at a time, read them all. */
function cookieJar(initial = {}) {
  const jar = new Map(Object.entries(initial));
  const written = [];
  return {
    written,
    get cookie() {
      return [...jar].map(([k, v]) => `${k}=${v}`).join("; ");
    },
    set cookie(line) {
      written.push(line);
      const [pair, ...attrs] = line.split(";").map((s) => s.trim());
      const [k, v] = [pair.slice(0, pair.indexOf("=")), pair.slice(pair.indexOf("=") + 1)];
      if (attrs.includes("Max-Age=0")) jar.delete(k);
      else jar.set(k, v);
    },
  };
}

railPrefsContract("memory", () => memoryRailPrefs());
railPrefsContract("cookie", () => cookieRailPrefs(cookieJar()));

test("the cookie is the one the server reads: comma-separated keys, the whole site, a year, Lax", () => {
  const doc = cookieJar({ theme: "dark" });
  cookieRailPrefs(doc).setCollapsed(["backlog", "done"]);
  assert.equal(doc.written.at(-1), "rail_collapsed=backlog,done; Path=/; Max-Age=31536000; SameSite=Lax");
  cookieRailPrefs(doc).setCollapsed([]);
  assert.equal(doc.written.at(-1), "rail_collapsed=; Path=/; Max-Age=0; SameSite=Lax");
});

test("a malformed cookie collapses nothing", () => {
  assert.deepEqual(cookieRailPrefs(cookieJar({ rail_collapsed: "Backlog,done" })).collapsed(), []);
  assert.deepEqual(cookieRailPrefs(cookieJar({ rail_collapsed: " backlog , done " })).collapsed(), ["backlog", "done"]);
});
