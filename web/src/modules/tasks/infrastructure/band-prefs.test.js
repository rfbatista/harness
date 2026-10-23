import { assert, file, test } from "../../../shared/testing/test.js";
import { cookieBandPrefs } from "./band-prefs.js";

file("tasks/infrastructure/band-prefs");

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
      const [pair] = line.split(";");
      jar.set(pair.slice(0, pair.indexOf("=")), pair.slice(pair.indexOf("=") + 1));
    },
  };
}

test("remembers nothing at first, then the panel that was left open, or none", () => {
  const doc = cookieJar({ theme: "dark" });
  const prefs = cookieBandPrefs(doc);
  assert.equal(prefs.open(), "");
  prefs.setOpen("feature");
  assert.equal(prefs.open(), "feature");
  prefs.setOpen("none");
  assert.equal(prefs.open(), "none");
});

test("the cookie is the one the server reads: one key, the whole site, a year, Lax", () => {
  const doc = cookieJar();
  cookieBandPrefs(doc).setOpen("token-savers");
  assert.equal(doc.written.at(-1), "task_band=token-savers; Path=/; Max-Age=31536000; SameSite=Lax");
});

test("an unknown or malformed value counts as unset, and an unknown key is not written", () => {
  for (const value of ["savers", "Feature", "reviews,none", ""]) assert.equal(cookieBandPrefs(cookieJar({ task_band: value })).open(), "", value);
  const doc = cookieJar();
  cookieBandPrefs(doc).setOpen("savers");
  assert.deepEqual(doc.written, []);
});
