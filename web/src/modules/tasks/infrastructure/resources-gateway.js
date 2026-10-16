// ResourceGateway over the harness HTTP API: the resource and comment routes,
// the multipart upload (with progress, through the uploader) and the
// `ticket_resource` / `ticket_comment` changes on the project feed.

import { toComment, toCommentList, toResource, toResourceChange, toResourceList, toResourcesSeed } from "./resource-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @param {import("../../../shared/infrastructure/upload.js").Uploader} upload
 * @returns {import("../domain/ports.js").ResourceGateway}
 */
export function resourcesGateway(api, feed, upload) {
  return {
    decodeSeed: toResourcesSeed,

    async listResources(ticketId) {
      return toResourceList(await api.get("/list_ticket_resources", { ticket_id: ticketId }));
    },

    async upload({ ticketId, file, title = "", onProgress, signal }) {
      // The server reads the parts in order and checks the task before the
      // file's first byte: ticket_id and title go first.
      const parts = [["ticket_id", ticketId]];
      if (title.trim()) parts.push(["title", title]);
      parts.push(["file", file, file.name]);
      const body = await upload("/upload_ticket_resource", parts, { onProgress, signal });
      return toResource(body?.resource);
    },

    async addLink({ ticketId, url, title = "" }) {
      const body = { ticket_id: ticketId, url };
      if (title.trim()) body.title = title;
      return toResource((await api.post("/add_ticket_link", body))?.resource);
    },

    async retitle(resourceId, title) {
      return toResource((await api.post("/update_ticket_resource", { resource_id: resourceId, title }))?.resource);
    },

    async removeResource(resourceId) {
      await api.post("/delete_ticket_resource", { resource_id: resourceId });
    },

    async listComments(ticketId) {
      return toCommentList(await api.get("/list_ticket_comments", { ticket_id: ticketId }));
    },

    async addComment(ticketId, body) {
      return toComment((await api.post("/add_ticket_comment", { ticket_id: ticketId, body }))?.comment);
    },

    async editComment(commentId, body) {
      return toComment((await api.post("/update_ticket_comment", { comment_id: commentId, body }))?.comment);
    },

    async removeComment(commentId) {
      await api.post("/delete_ticket_comment", { comment_id: commentId });
    },

    follow(projectId, onChange, onStatus) {
      return feed.followPage({ kind: "project", projectId }, {
        onMessage: (dto) => {
          let change;
          try {
            change = toResourceChange(dto);
          } catch {
            return; // a change we cannot read is dropped; the next resync corrects the lists
          }
          if (change) onChange(change);
        },
        onStatus,
      });
    },
  };
}
