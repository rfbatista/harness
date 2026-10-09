// Resources: what a person adds to a task for context — an uploaded file,
// image or audio, or a link. Mirrors internal/domain/ticket_resource.go and
// Contract: Harness server ↔ Web UI — task resources and comments. Never
// "attachments": that word belongs to project design assets attached to a task.

/**
 * @typedef {"file"|"image"|"audio"|"link"} ResourceKind
 *
 * @typedef {object} Resource
 * @property {string} id
 * @property {string} ticketId
 * @property {string} projectId
 * @property {ResourceKind} kind
 * @property {string} title
 * @property {string} mime        "" for a link
 * @property {number} sizeBytes   0 for a link
 * @property {string} url         the link target; "" for an upload
 * @property {string} contentUrl  where the bytes are served; "" for a link
 * @property {Date} createdAt
 * @property {Date} updatedAt
 */

export const KINDS = Object.freeze(["file", "image", "audio", "link"]);

/** The server refuses a bigger upload with RESOURCE_TOO_LARGE. */
export const MAX_RESOURCE_BYTES = 25 * 1024 * 1024;
export const MAX_TITLE = 200;
export const MAX_URL = 2048;

const KIB = 1024;
const MIB = 1024 * KIB;
const GIB = 1024 * MIB;

/** "512 B", "403 KB", "1.8 MB", "2.0 GB": binary units, KB rounded, MB and up to one decimal. */
export function sizeWords(n) {
  const b = Math.max(0, n);
  if (b < KIB) return `${Math.round(b)} B`;
  if (b < MIB) return `${Math.round(b / KIB)} KB`;
  if (b < GIB) return `${(b / MIB).toFixed(1)} MB`;
  return `${(b / GIB).toFixed(1)} GB`;
}

/** Runes, not UTF-16 units: the server counts runes. */
export const runeCount = (s) => [...s].length;

/** The media type without its parameters, lower-cased: "text/plain; charset=utf-8" → "text/plain". */
export const baseMime = (mime) => (mime ?? "").split(";")[0].trim().toLowerCase();

/** The server serves SVG as an attachment, so it has no preview: it downloads like a file. */
export const isSvg = (r) => baseMime(r.mime) === "image/svg+xml";

/** Files the browser's own viewer opens safely in a new tab (the server serves them inline). */
const OPENS_IN_TAB = new Set(["application/pdf", "text/plain"]);

/** What the title does: open the lightbox, open a tab, download, follow the link, or nothing (audio plays in the row). */
export function titleAction(r) {
  if (r.kind === "link") return "link";
  if (r.kind === "image" && !isSvg(r)) return "lightbox";
  if (r.kind === "audio") return "none";
  if (OPENS_IN_TAB.has(baseMime(r.mime))) return "tab";
  return "download";
}

/** The type tile: the image itself, the audio or link glyph, or the file's extension. */
export function tileKind(r) {
  if (r.kind === "link") return "link";
  if (r.kind === "audio") return "audio";
  if (r.kind === "image" && !isSvg(r)) return "image";
  return "ext";
}

const SUBTYPE_NAMES = { jpeg: "JPEG", "svg+xml": "SVG", mpeg: "MP3", "octet-stream": "FILE", plain: "TXT", mp4: "MP4" };

/** "PDF", "JSON", "SVG": the title's extension, else a name from the MIME subtype. */
export function extensionLabel(title, mime) {
  const m = /\.([A-Za-z0-9]{1,5})$/.exec(title ?? "");
  if (m) return m[1].toUpperCase();
  const sub = baseMime(mime).split("/")[1] ?? "";
  if (!sub) return "FILE";
  if (SUBTYPE_NAMES[sub]) return SUBTYPE_NAMES[sub];
  const name = sub.replace(/^(x-|vnd\.)/, "").split(/[+.-]/).at(-1) ?? "";
  return name ? name.slice(0, 5).toUpperCase() : "FILE";
}

/** "PNG image", "M4A audio", "PDF": what the meta line calls the type. */
export function typeWord(r) {
  const ext = extensionLabel(r.title, r.mime);
  if (r.kind === "image") return `${ext} image`;
  if (r.kind === "audio") return `${ext} audio`;
  return ext;
}

/** A link's address as the meta shows it: host and path, no scheme. */
export function addressWords(url) {
  try {
    const u = new URL(url);
    const path = u.pathname === "/" ? "" : u.pathname;
    return `${u.host}${path}${u.search}`;
  } catch {
    return url;
  }
}

/** Where the title goes, or "" when it is not a link. */
export function titleHref(r) {
  switch (titleAction(r)) {
    case "link":
      return r.url;
    case "lightbox":
    case "tab":
      return r.contentUrl;
    case "download":
      return downloadHref(r);
    default:
      return "";
  }
}

/** The forced download of an upload's bytes; "" for a link. */
export const downloadHref = (r) => (r.contentUrl ? `${r.contentUrl}?download=1` : "");

/**
 * The row menu's items, in order. Delete is last (danger, after a separator).
 * @returns {("view"|"open"|"open-link"|"copy-address"|"download"|"rename"|"delete")[]}
 */
export function menuItems(r) {
  switch (titleAction(r)) {
    case "link":
      return ["open-link", "copy-address", "rename", "delete"];
    case "lightbox":
      return ["view", "download", "rename", "delete"];
    case "tab":
      return ["open", "download", "rename", "delete"];
    default:
      return ["download", "rename", "delete"];
  }
}

/** Why a link cannot be added, or "". Only http and https with a host, as the server accepts. */
export function linkProblem(url) {
  const value = (url ?? "").trim();
  if (runeCount(value) > MAX_URL) return "Use an address of at most 2,048 characters.";
  let u;
  try {
    u = new URL(value);
  } catch {
    return "Use an address that starts with http:// or https://.";
  }
  if ((u.protocol !== "http:" && u.protocol !== "https:") || !u.hostname) return "Use an address that starts with http:// or https://.";
  return "";
}

/** Why a title cannot be saved, or "". It is trimmed. */
export function titleProblem(title) {
  const value = (title ?? "").trim();
  if (!value) return "A resource needs a title.";
  if (runeCount(value) > MAX_TITLE) return "Use a title of at most 200 characters.";
  return "";
}

export const tooLarge = (size) => size > MAX_RESOURCE_BYTES;

/** "Pasted image 14:07.png": the title a pasted screenshot gets, which has no useful name of its own. */
export function pastedTitle(now, mime) {
  const hh = String(now.getHours()).padStart(2, "0");
  const mm = String(now.getMinutes()).padStart(2, "0");
  const sub = baseMime(mime).split("/")[1] ?? "";
  const ext = sub === "jpeg" ? "jpg" : sub.replace(/\W.*$/, "") || "png";
  return `Pasted image ${hh}:${mm}.${ext}`;
}

/** The images the lightbox steps through, in list order: every image the browser can show. */
export const viewableImages = (resources) => resources.filter((r) => titleAction(r) === "lightbox");
