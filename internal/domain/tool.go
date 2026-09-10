package domain

// Tools are implemented by github.com/rfbatista/harnesskit/tool. These are type
// aliases, so domain.Tool and tool.Tool are the same type.

import "github.com/rfbatista/harnesskit/tool"

type (
	// ToolHandlerFunc is the signature for a code-defined tool's execution logic.
	ToolHandlerFunc = tool.HandlerFunc
	// Tool pairs a typed input schema with a description and, for code-defined
	// tools, a handler. Source is "code" or "user"; Handler is transient and
	// never persisted.
	Tool = tool.Tool
)
