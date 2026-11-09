// The service form as a draft: every field is text the person types, and
// toInput() turns it into the API's ServiceInput (contract: Harness server ↔
// Web UI — services, section 3). The server validates; the form only names the
// field a refusal points at.

const emptyMapping = () => ({ host: "", container: "", protocol: "tcp" });
/** A row of a list with one text field: its own object, so a field can bind to it. */
const row = (value = "") => ({ value });

/** @typedef {"command" | "image"} Kind */

/**
 * @typedef {object} Draft
 * @property {string} name
 * @property {Kind} kind
 * @property {string} repositoryId
 * @property {boolean} exportRepositoryEnv
 * @property {string} env
 * @property {string} line  command service: the command line
 * @property {string} subdir
 * @property {{ value: string }[]} ports  command service: informational ports, as typed
 * @property {string} image  image service: the image reference
 * @property {{ value: string }[]} args
 * @property {{ host: string, container: string, protocol: "tcp" | "udp" }[]} mappings
 */

/** @returns {Draft} */
export function newDraft(kind = "command") {
  return {
    name: "", kind, repositoryId: "", exportRepositoryEnv: false, env: "",
    line: "", subdir: "", ports: [row()], image: "", args: [], mappings: [emptyMapping()],
  };
}

/** @returns {Draft} */
export function draftFromService(service) {
  const d = newDraft(service.kind);
  d.name = service.name;
  d.repositoryId = service.repositoryId;
  d.exportRepositoryEnv = service.exportRepositoryEnv;
  d.env = service.env;
  if (service.command) {
    d.line = service.command.line;
    d.subdir = service.command.subdir;
    d.ports = service.command.ports.length > 0 ? service.command.ports.map((p) => row(String(p))) : [row()];
  }
  if (service.image) {
    d.image = service.image.ref;
    d.args = service.image.args.map(row);
    d.mappings = service.image.ports.length > 0
      ? service.image.ports.map((p) => ({ host: String(p.host), container: String(p.container), protocol: p.protocol }))
      : [emptyMapping()];
  }
  return d;
}

/** The kind of a draft that is still new: what both kinds share stays. */
export function setKind(draft, kind) {
  return { ...draft, kind };
}

/** A port as the server reads it: a number. Text that is not one is 0, so the server names it. */
const portNumber = (text) => (/^\d+$/.test(text.trim()) ? Number(text.trim()) : 0);

/** The API's ServiceInput. */
export function toInput(draft) {
  const repository_id = draft.repositoryId;
  const base = {
    name: draft.name.trim(),
    kind: draft.kind,
    repository_id,
    export_repository_env: repository_id !== "" && draft.exportRepositoryEnv,
    env: draft.env,
  };
  if (draft.kind === "image") {
    return {
      ...base,
      command: null,
      image: {
        ref: draft.image.trim(),
        args: draft.args.map((a) => a.value).filter((a) => a !== ""),
        ports: draft.mappings
          .filter((m) => m.host.trim() !== "" || m.container.trim() !== "")
          .map((m) => ({ host: portNumber(m.host), container: portNumber(m.container), protocol: m.protocol })),
      },
    };
  }
  return {
    ...base,
    command: {
      line: draft.line.trim(),
      subdir: draft.subdir.trim(),
      ports: draft.ports.map((p) => p.value).filter((p) => p.trim() !== "").map(portNumber),
    },
    image: null,
  };
}

export function addRow(draft, list) {
  return { ...draft, [list]: [...draft[list], list === "mappings" ? emptyMapping() : row()] };
}

/** Removes row `index`; a list is never left without a row to type in, except the arguments, which may be none. */
export function removeRow(draft, list, index) {
  const rows = draft[list].filter((_, i) => i !== index);
  if (rows.length === 0 && list !== "args") rows.push(list === "mappings" ? emptyMapping() : row());
  return { ...draft, [list]: rows };
}

const FIELD_OF = [
  [/^name\b/, "name"],
  [/^env\b/, "env"],
  [/^kind\b/, "kind"],
  [/^command\.line\b/, "line"],
  [/^command\.subdir\b/, "subdir"],
  [/^command\.ports\[(\d+)\]/, "ports"],
  [/^image\.ref\b/, "image"],
  [/^image\.ports\[(\d+)\]/, "mappings"],
];

/** The field an INVALID_INPUT message names, and the row for a list; field "" when it names none. */
export function fieldOf(message) {
  for (const [pattern, field] of FIELD_OF) {
    const m = pattern.exec(message);
    if (m) return { field, index: m[1] === undefined ? -1 : Number(m[1]) };
  }
  return { field: "", index: -1 };
}

export const formTitle = (service) => (service ? `Edit ${service.name}` : "New service");

/** Whether the required fields hold something; the server still validates the rest. */
export function canSave(draft) {
  if (draft.name.trim() === "") return false;
  return draft.kind === "image" ? draft.image.trim() !== "" : draft.line.trim() !== "";
}
