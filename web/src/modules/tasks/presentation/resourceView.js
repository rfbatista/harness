// How a task's resources, uploads and comments read on the page: the plain
// values the markup binds. Pure; the page component decides the state.

import { count } from "../../../shared/presentation/format.js";
import { isEdited } from "../domain/comments.js";
import { addressWords, downloadHref, extensionLabel, menuItems, sizeWords, tileKind, titleAction, titleHref, typeWord } from "../domain/resources.js";
import { percent, progressWords, uploadTitle, UploadState } from "../domain/uploads.js";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "just now", "12 min ago", "3 h ago", "2 d ago". */
export function agoWords(then, now) {
  const ms = Math.max(0, now.getTime() - then.getTime());
  if (ms < MINUTE) return "just now";
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)} min ago`;
  if (ms < DAY) return `${Math.floor(ms / HOUR)} h ago`;
  return `${Math.floor(ms / DAY)} d ago`;
}

/** The full date, for a tooltip. */
export const fullDate = (d) => d.toLocaleString("en-GB", { dateStyle: "medium", timeStyle: "short" });

const MENU_LABELS = {
  view: "View",
  open: "Open in a new tab",
  "open-link": "Open link",
  "copy-address": "Copy address",
  download: "Download",
  rename: "Rename",
  delete: "Delete…",
};

/**
 * The row menu's items. An item that goes somewhere is a link; the rest run
 * an action on the page.
 * @param {import("../domain/resources.js").Resource} r
 */
function menuView(r) {
  return menuItems(r).map((action) => {
    const item = { action, label: MENU_LABELS[action], href: "", newTab: false, download: false, danger: action === "delete" };
    if (action === "open") Object.assign(item, { href: r.contentUrl, newTab: true });
    if (action === "open-link") Object.assign(item, { href: r.url, newTab: true });
    if (action === "download") Object.assign(item, { href: downloadHref(r), download: true });
    return item;
  });
}

/**
 * @param {import("../domain/resources.js").Resource} r
 * @param {{ now: Date, fresh: boolean, renaming: boolean, confirming: boolean, busy: boolean }} state
 */
export function resourceRow(r, { now, fresh = false, renaming = false, confirming = false, busy = false }) {
  const action = titleAction(r);
  const meta = r.kind === "link" ? "" : `${typeWord(r)} · ${sizeWords(r.sizeBytes)} · ${agoWords(r.createdAt, now)}`;
  return {
    id: r.id,
    kind: r.kind,
    title: r.title,
    tile: tileKind(r),
    ext: extensionLabel(r.title, r.mime),
    thumb: tileKind(r) === "image" ? r.contentUrl : "",
    action,
    /** The title is a link (a tab, a download or the link itself); else a button (lightbox) or text (audio). */
    href: titleHref(r),
    newTab: action === "tab" || action === "link",
    download: action === "download",
    isLightbox: action === "lightbox",
    isLink: action === "tab" || action === "download" || action === "link",
    isAudio: r.kind === "audio",
    audioSrc: r.kind === "audio" ? r.contentUrl : "",
    meta,
    host: r.kind === "link" ? addressWords(r.url) : "",
    metaTitle: fullDate(r.createdAt),
    downloadHref: downloadHref(r),
    viewLabel: `View ${r.title}`,
    downloadLabel: `Download ${r.title}`,
    playLabel: `Play ${r.title}`,
    moreLabel: `More actions for ${r.title}`,
    deleteQuestion: `Delete “${r.title}”?`,
    menu: menuView(r),
    fresh,
    renaming,
    confirming,
    busy,
  };
}

/** @param {import("../domain/uploads.js").UploadRow} u */
export function uploadRow(u) {
  const failed = u.state === UploadState.FAILED;
  return {
    key: u.key,
    title: uploadTitle(u),
    ext: extensionLabel(u.name, ""),
    state: u.state,
    failed,
    meta: failed ? u.failure.message : progressWords(u),
    code: failed ? `${u.failure.code} · ${u.failure.next}` : "",
    canRetry: failed && u.failure.retry,
    uploading: u.state === UploadState.UPLOADING,
    percent: percent(u),
    progressLabel: `Uploading ${uploadTitle(u)}`,
  };
}

/**
 * @param {import("../domain/comments.js").Comment} c
 * @param {{ now: Date, fresh: boolean, editing: boolean, confirming: boolean, busy: boolean }} state
 */
export function commentRow(c, { now, fresh = false, editing = false, confirming = false, busy = false }) {
  const age = agoWords(c.createdAt, now);
  return {
    id: c.id,
    body: c.body,
    age,
    datetime: c.createdAt.toISOString(),
    dateTitle: fullDate(c.createdAt),
    edited: isEdited(c),
    editedTitle: `Edited ${fullDate(c.updatedAt)}`,
    editLabel: `Edit comment from ${age}`,
    deleteLabel: `Delete comment from ${age}`,
    fresh,
    editing,
    confirming,
    busy,
  };
}

/** "6 resources · 3 comments". */
export const summaryWords = (resources, comments) => `${count(resources, "resource")} · ${count(comments, "comment")}`;
