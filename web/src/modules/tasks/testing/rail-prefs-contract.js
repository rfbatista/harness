// The RailPrefs contract (../domain/ports.js), run by every implementation.

import { assert, test } from "../../../shared/testing/test.js";

/** @param {string} name @param {() => import("../domain/ports.js").RailPrefs} make */
export function railPrefsContract(name, make) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);

  contract("remembers nothing at first", () => {
    assert.deepEqual(make().collapsed(), []);
  });

  contract("remembers the collapsed groups, once each", () => {
    const prefs = make();
    prefs.setCollapsed(["backlog", "done", "done"]);
    assert.deepEqual(prefs.collapsed(), ["backlog", "done"]);
    prefs.setCollapsed(["done"]);
    assert.deepEqual(prefs.collapsed(), ["done"]);
  });

  contract("forgets them all", () => {
    const prefs = make();
    prefs.setCollapsed(["backlog"]);
    prefs.setCollapsed([]);
    assert.deepEqual(prefs.collapsed(), []);
  });
}
