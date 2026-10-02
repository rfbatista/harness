---
name: browser-feature-testing
description: Use when asked to verify a feature works in the browser, test UI behavior, check if a page renders correctly, validate user flows, or confirm a fix works visually. Triggers on phrases like "check if X works", "test the feature", "verify in the browser", "does this work?", "open the app and test".
---

# Browser Feature Testing

Use `dev-browser` to test features by driving a real browser, not by reading code.

## Core Workflow

```
0. Login (if .dev-browser/login.js exists) → 1. Navigate → 2. Snapshot → 3. Interact → 4. Assert → 5. Screenshot on failure
```

Always run a snapshot first on unknown pages to discover element roles and names before interacting.

## Step 0 — Start the dev server (if needed)

Before testing, ensure the dev server is running. If it's not, ask the user or check if a dev server command is configured in `package.json`.

## Step 0b — Login / Session Setup (per-project)

**Before any feature test, check if `.dev-browser/login.js` exists in the project root.** If it does, run it first:

```bash
dev-browser run .dev-browser/login.js
```

This script logs in once and stores the session in a named page `"session"`. All subsequent pages share the same browser context (and therefore the same cookies), so they are automatically authenticated.

**If the file doesn't exist**, ask the user whether login is required. If it is, help them create `.dev-browser/login.js` using this template:

```js
// .dev-browser/login.js
// Reusable login setup — run before any feature test.
// Named page "session" persists between runs. If already logged in, skip.

const page = await browser.getPage("session");
const url = page.url();

// Adjust this condition to match your app's authenticated URL pattern
if (url.includes("/dashboard") || url.includes("/home") || url.includes("/app")) {
  console.log(JSON.stringify({ alreadyLoggedIn: true, url }));
} else {
  await page.goto("http://localhost:3000/login", { waitUntil: "domcontentloaded" });

  // Snapshot to discover the login form selectors if needed:
  // const snap = await page.snapshotForAI(); console.log(snap.full);

  await page.fill('[name="email"]', "your-test-user@example.com");
  await page.fill('[name="password"]', "your-test-password");
  await page.getByRole("button", { name: "Entrar" }).click();

  // Wait for redirect to authenticated area
  await page.waitForURL("**/dashboard", { timeout: 10000 });
  console.log(JSON.stringify({ loggedIn: true, url: page.url() }));
}
```

**Why this works:** `browser.getPage("session")` creates a persistent named tab. When you later open `browser.getPage("feature-test")`, both tabs share the same browser context — cookies, localStorage, and session tokens are shared automatically.

**Add `.dev-browser/login.js` to `.gitignore`** if it contains real credentials.

## Step 1 — Navigate to the feature

Use `--headless` for automated/unattended testing (no visible browser window):

```bash
dev-browser --headless <<'EOF'
const page = await browser.getPage("feature-test");
await page.goto("http://localhost:3000/your-path", { waitUntil: "domcontentloaded" });
console.log(JSON.stringify({ url: page.url(), title: await page.title() }));
EOF
```

Omit `--headless` if you want to watch the browser window during debugging.

**Always use `waitUntil: "domcontentloaded"`** on dev servers (Next.js, Vite, etc.) — the default `"load"` can hang on HMR connections.

## Step 2 — Discover the page

When you don't know exact selectors, snapshot first:

```bash
dev-browser <<'EOF'
const page = await browser.getPage("feature-test");
const snap = await page.snapshotForAI();
console.log(snap.full);
EOF
```

Read the snapshot output to identify element roles, names, and structure. **Never guess selectors** — snapshot first.

## Step 3 — Interact

Use `getByRole` from snapshot info (most reliable) or Playwright selectors:

```bash
dev-browser <<'EOF'
const page = await browser.getPage("feature-test");
// From snapshot: button with name "Avaliar Aula"
await page.getByRole("button", { name: "Avaliar Aula" }).click();
// Or use selectors:
await page.fill('[data-testid="email"]', "test@example.com");
await page.press('[data-testid="email"]', "Enter");
console.log("interacted");
EOF
```

## Step 4 — Assert

Log the state you need to verify:

```bash
dev-browser <<'EOF'
const page = await browser.getPage("feature-test");
// Wait for expected outcome
await page.waitForSelector('[data-testid="success-message"]');
const text = await page.textContent('[data-testid="success-message"]');
const url = page.url();
console.log(JSON.stringify({ text, url, passed: text.includes("sucesso") }));
EOF
```

Common assertions:
- **Element visible:** `await page.waitForSelector(".modal")`
- **URL changed:** `await page.waitForURL("**/dashboard")`
- **Text content:** `await page.textContent(selector)`
- **Element count:** `await page.$$eval("li", els => els.length)`

## Step 5 — Screenshot on failure or for visual check

```bash
dev-browser <<'EOF'
const page = await browser.getPage("feature-test");
const buf = await page.screenshot();
const path = await saveScreenshot(buf, "feature-check.png");
console.log(path);
EOF
```

Read the screenshot path printed, then use the Read tool to view the image.

## Quick Reference

| Goal | Command |
|------|---------|
| Navigate (dev server) | `page.goto(url, { waitUntil: "domcontentloaded" })` |
| Discover elements | `page.snapshotForAI()` |
| Click by role | `page.getByRole("button", { name: "..." }).click()` |
| Fill input | `page.fill(selector, value)` |
| Wait for element | `page.waitForSelector(selector)` |
| Wait for URL | `page.waitForURL("**/path")` |
| Get text | `page.textContent(selector)` |
| Take screenshot | `saveScreenshot(await page.screenshot(), "name.png")` |
| Re-inspect after error | list current URL + screenshot |

## Error Recovery

If a script fails, the page stays where it stopped. Reconnect, screenshot, and log state:

```bash
dev-browser <<'EOF'
const page = await browser.getPage("feature-test");
const path = await saveScreenshot(await page.screenshot(), "debug.png");
console.log(JSON.stringify({ screenshot: path, url: page.url(), title: await page.title() }));
EOF
```

## Named Pages Persist

`browser.getPage("feature-test")` creates or reconnects to the same tab across multiple `dev-browser` calls. **Keep the page name consistent** across all steps in a test session so you don't re-navigate unnecessarily.

## Testing Checklist

- [ ] Navigate to the feature URL
- [ ] Snapshot to discover elements (if selectors unknown)
- [ ] Interact with the feature (click, fill, submit)
- [ ] Wait for and assert the expected outcome
- [ ] Take a screenshot if visual confirmation is needed
- [ ] Report: passed/failed + what was verified + screenshot path if taken

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Using `waitUntil: "load"` on dev servers | Use `"domcontentloaded"` |
| Guessing selectors | Snapshot first with `snapshotForAI()` |
| Writing TypeScript in `page.evaluate()` | Plain JS only inside `evaluate` |
| Using `require()` or `import` | Not available — QuickJS sandbox |
| Not awaiting file I/O | `saveScreenshot`, `writeFile`, `readFile` are all async |
| Single script doing everything | Break into small focused scripts per step |
