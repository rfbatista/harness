package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// productOwnerMemoryTools are the tools with which the product owner lists,
// saves, changes and drops the entries of its project's memory. Each acts on
// the project of the thread its session answers for.
func productOwnerMemoryTools(mem ports.ProductOwnerMemoryTools) []domain.Tool {
	call := func(fn func(ctx context.Context, sessionID string, args map[string]any) (any, error)) func(context.Context, map[string]any) (any, error) {
		return func(ctx context.Context, args map[string]any) (any, error) {
			if mem == nil {
				return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "memory is not available on this server"}
			}
			id := SessionIDFrom(ctx)
			if id == "" {
				return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no session in context"}
			}
			return fn(ctx, id, args)
		}
	}
	return []domain.Tool{
		{
			Name: "list_memory",
			Description: "List this project's memory: every entry, the newest first, with its id, text, when it was saved and " +
				"changed, and the thread it came from. Entries from other threads and edits the person made show here.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: call(func(ctx context.Context, sessionID string, _ map[string]any) (any, error) {
				entries, err := mem.ListMemory(ctx, sessionID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"count": len(entries), "entries": entries}, nil
			}),
		},
		{
			Name: "save_memory",
			Description: "Save one durable fact to this project's memory: a decision, a preference of the person, what was agreed. " +
				"Text is at most 4,000 characters. The thread shows a \"saved to memory\" note. Tell the person in your answer " +
				"that you saved it.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["text"],"properties":{"text":{"type":"string","description":"The fact, in a sentence or two, that a later conversation can rely on."}}}`),
			Source:      "code",
			Handler: call(func(ctx context.Context, sessionID string, args map[string]any) (any, error) {
				return mem.SaveMemory(ctx, sessionID, getString(args, "text", ""))
			}),
		},
		{
			Name:        "change_memory",
			Description: "Replace the text of one memory entry, by its id from list_memory. Tell the person in your answer that you changed it.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["entry_id","text"],"properties":{"entry_id":{"type":"string","description":"The entry's id, from list_memory."},"text":{"type":"string","description":"The new text, at most 4,000 characters."}}}`),
			Source:      "code",
			Handler: call(func(ctx context.Context, sessionID string, args map[string]any) (any, error) {
				return mem.ChangeMemory(ctx, sessionID, getString(args, "entry_id", ""), getString(args, "text", ""))
			}),
		},
		{
			Name:        "drop_memory",
			Description: "Drop one memory entry that is no longer true, by its id from list_memory. Tell the person in your answer that you dropped it.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["entry_id"],"properties":{"entry_id":{"type":"string","description":"The entry's id, from list_memory."}}}`),
			Source:      "code",
			Handler: call(func(ctx context.Context, sessionID string, args map[string]any) (any, error) {
				id := getString(args, "entry_id", "")
				if err := mem.DropMemory(ctx, sessionID, id); err != nil {
					return nil, err
				}
				return map[string]any{"entry_id": id, "dropped": true}, nil
			}),
		},
	}
}
