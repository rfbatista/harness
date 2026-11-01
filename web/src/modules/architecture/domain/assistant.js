// The architecture assistant's conversation as the panel shows it: the
// progress line for a tool use, and the message list's own rules. Pure.

/** The codes the assistant's turn ends with that the panel acts on. */
export const ASSISTANT_CONVERSATION_NOT_FOUND = "CONVERSATION_NOT_FOUND";
export const ASSISTANT_UNAVAILABLE = "ARCHITECTURE_ASSISTANT_UNAVAILABLE";
export const ASSISTANT_FAILED = "ARCHITECTURE_ASSISTANT_FAILED";

const VERBS = { Read: "Reading", Grep: "Searching", Glob: "Listing" };

/** The progress line for one `activity` event: "Reading internal/domain/zone.go". */
export function activityText({ tool, target }) {
  return `${VERBS[tool] ?? tool} ${target}`.trim();
}

/** A request body for one turn of the conversation, from what the page holds. */
export function turnRequest({ repositoryId, conversationId, message, draft, baseVersion, ruleEventId }) {
  const body = { repository_id: repositoryId, message, draft, draft_base_version: baseVersion };
  if (conversationId) body.conversation_id = conversationId;
  if (ruleEventId) body.rule_event_id = ruleEventId;
  return body;
}
