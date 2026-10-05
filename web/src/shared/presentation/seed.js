// The server embeds a page's data with templ.JSONScript and the component's
// root points at it: <main x-data="sessionsPage" data-seed="sessions-seed">.

/**
 * Parses the JSON seed `el` points at through data-seed.
 * @param {HTMLElement} el
 * @param {Document} [doc]
 */
export function readSeed(el, doc = el.ownerDocument) {
  const id = el.dataset.seed;
  if (!id) throw new Error(`${describe(el)} has no data-seed attribute`);
  const script = doc.getElementById(id);
  if (!script) throw new Error(`seed #${id} not found for ${describe(el)}`);
  return JSON.parse(script.textContent ?? "null");
}

function describe(el) {
  return `<${el.tagName.toLowerCase()} x-data="${el.getAttribute("x-data") ?? ""}">`;
}
