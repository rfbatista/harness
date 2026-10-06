// Artifacts as the Design tab's markup binds them: the card in the list and
// the preview of the selected one. Pure; no DOM.

import { bytes, relativeTime } from "../../../shared/presentation/format.js";
import { fileName, isLoopbackUrl, Kind } from "../domain/artifact.js";

const KIND_WORD = { page: "page", image: "image", video: "video", url: "dev server", file: "file" };

/** How a kind reads on a card. */
export const kindWord = (kind) => KIND_WORD[kind] ?? kind;

/** The title, or the file's name, or the url, or a placeholder. Rendered as text only. */
export function artifactTitle(artifact) {
  return artifact.title || fileName(artifact) || artifact.url || "Untitled artifact";
}

export function toCardView(artifact, { selectedId, now, fresh = false }) {
  return {
    id: artifact.id,
    title: artifactTitle(artifact),
    kindWord: kindWord(artifact.kind),
    note: artifact.note,
    revision: `rev ${artifact.revision}`,
    updated: relativeTime(artifact.updatedAt, now),
    selected: artifact.id === selectedId,
    fresh,
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
    openHref: isUrl ? src : artifact.src,
    fileName: fileName(artifact),
    size: bytes(artifact.sizeBytes),
    mime: artifact.mime,
    revision: artifact.revision,
    updated: relativeTime(artifact.updatedAt, now),
    note: artifact.note,
  };
}
