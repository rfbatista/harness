// Reads an island's props: the JSON in <script type="application/json" data-island-props="name">.

export class IslandPropsError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "IslandPropsError";
  }
}

export function readIslandProps(root: ParentNode, name: string): unknown {
  const script = root.querySelector(`script[data-island-props="${name}"]`);
  if (!script) throw new IslandPropsError(`no props script for island ${name}`);
  try {
    return JSON.parse(script.textContent ?? "") as unknown;
  } catch {
    throw new IslandPropsError(`the props of island ${name} are not JSON`);
  }
}

/** The JSON of the `<script type="application/json" id="…">` a page embeds for its own components (a seed). */
export function readScriptJson(doc: Document, id: string): unknown {
  const script = doc.getElementById(id);
  if (!script) throw new IslandPropsError(`no script with the id ${id}`);
  try {
    return JSON.parse(script.textContent ?? "") as unknown;
  } catch {
    throw new IslandPropsError(`the script ${id} is not JSON`);
  }
}
