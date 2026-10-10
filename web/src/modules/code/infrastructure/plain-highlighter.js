// A Highlighter that leaves code plain: for tests, and wherever no grammar is
// registered.

/** @returns {import("../domain/ports.js").Highlighter} */
export function plainHighlighter() {
  return {
    supports: () => false,
    tokenize: (_language, text) => text.split("\n").map((t) => (t ? [{ text: t, tok: "" }] : [])),
  };
}
