// Artifacts as the Design tab's markup binds them: the card in the list and
// the preview of the selected one. Pure; no DOM.

import { bytes, relativeTime } from "../../../shared/presentation/format.js";
import { fileName, isLoopbackUrl, isPromotable, Kind, relationTo, Scope } from "../domain/artifact.js";

const KIND_WORD = { page: "page", image: "image", video: "video", url: "dev server", file: "file" };

/** A task's design assets page: what its sessions made and what is attached to it. */
export const taskDesignHref = (projectId, ticketId) => `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(ticketId)}/design`;

/** How a kind reads on a card. */
export const kindWord = (kind) => KIND_WORD[kind] ?? kind;

/** The title, or the file's name, or the url, or a placeholder. Rendered as text only. */
export function artifactTitle(artifact) {
  return artifact.title || fileName(artifact) || artifact.url || "Untitled artifact";
}

/** "attached to 2 tasks"; "" when it is on none. */
export function attachedWord(artifact) {
  const n = artifact.attachedTicketIds.length;
  return n === 0 ? "" : n === 1 ? "attached to 1 task" : `attached to ${n} tasks`;
}

/**
 * The question a move back to its task asks first, when it is attached
 * elsewhere: "" when it is not, and the move needs no confirmation.
 * @param {string[]} attachedTitles  the titles of the tasks it is attached to
 */
export function moveBackWarning(artifact, producerTitle, attachedTitles) {
  const n = artifact.attachedTicketIds.length;
  if (n === 0) return "";
  const tasks = n === 1 ? "1 task" : `${n} tasks`;
  return `Move ${artifactTitle(artifact)} back to ${producerTitle}? It will be detached from ${tasks}: ${attachedTitles.join(", ")}.`;
}

/** A picker's choices narrowed by what the person typed: every word, anywhere in the label, any case. */
export function filterChoices(choices, query) {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  return choices.filter((c) => {
    const label = c.label.toLowerCase();
    return words.every((w) => label.includes(w));
  });
}

/**
 * @param {{ selectedId: string, now: Date, fresh?: boolean, ticketId?: string }} opts
 *        ticketId: the task the list is about, for its relation to each card
 */
export function toCardView(artifact, { selectedId, now, fresh = false, ticketId = "" }) {
  const relation = ticketId ? relationTo(artifact, ticketId) : null;
  return {
    id: artifact.id,
    title: artifactTitle(artifact),
    kindWord: kindWord(artifact.kind),
    note: artifact.note,
    revision: `rev ${artifact.revision}`,
    updated: relativeTime(artifact.updatedAt, now),
    selected: artifact.id === selectedId,
    fresh,
    /** The list's mark on a project artifact; nothing on a task one. */
    scopeMark: artifact.scope === Scope.PROJECT ? "project" : "",
    /** On a task's list: the project asset came from another task. */
    attachedMark: relation === "attached" ? "attached" : "",
    attachedWord: attachedWord(artifact),
  };
}

/**
 * The selected artifact, as the preview renders it. `key` changes with the
 * revision, so the element is remounted and reloads; `src` carries the
 * revision as a query so a cached copy is never shown. A url is embedded only
 * when it points at this machine.
 */
export function toPreviewView(artifact, now) {
  const title = artifactTitle(artifact);
  const isUrl = artifact.kind === Kind.URL;
  const embed = isUrl ? isLoopbackUrl(artifact.url) : true;
  const src = isUrl ? (embed ? artifact.url : "") : `${artifact.src}?rev=${artifact.revision}`;
  return {
    key: `${artifact.id}@${artifact.revision}`,
    id: artifact.id,
    kind: artifact.kind,
    kindWord: kindWord(artifact.kind),
    title,
    frameTitle: `${title}, revision ${artifact.revision}`,
    src,
    embed,
    notEmbeddable: isUrl && !embed,
    /** CSP markup cannot write `isUrl && embed`; the url frame binds this. */
    embedsUrl: isUrl && embed,
    isPage: artifact.kind === Kind.PAGE,
    isImage: artifact.kind === Kind.IMAGE,
    isVideo: artifact.kind === Kind.VIDEO,
    isFile: artifact.kind === Kind.FILE,
    isUrl,
    /** The url as text, shown even when it is not embedded. */
    url: artifact.url,
    openHref: isUrl ? src : artifact.src,
    fileName: fileName(artifact),
    size: bytes(artifact.sizeBytes),
    mime: artifact.mime,
    revision: artifact.revision,
    updated: relativeTime(artifact.updatedAt, now),
    note: artifact.note,
    ...scopeView(artifact, title),
  };
}

/** The scope in words (never by colour alone) and the move that fits it. */
function scopeView(artifact, title) {
  const isProject = artifact.scope === Scope.PROJECT;
  return {
    scope: artifact.scope,
    isProject,
    scopeWord: isProject ? "Project asset" : "Task asset",
    /** Back to task always; to the project only with a file to keep. */
    canMove: isProject || isPromotable(artifact),
    moveTarget: isProject ? Scope.TASK : Scope.PROJECT,
    moveLabel: isProject ? "Move back to task" : "Move to project",
    moveAriaLabel: isProject ? `Move ${title} back to task` : `Move ${title} to project`,
  };
}
