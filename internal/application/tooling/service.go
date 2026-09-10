package tooling

import "github.com/rfbatista/harnesskit/tool"

// Service is the registry for source-code (built-in) tools. It is an alias for
// tool.Registry, which owns the implementation: tools are registered at startup
// and the registry is read-only afterwards.
type Service = tool.Registry

// NewService returns an empty tool registry.
func NewService() *Service { return tool.NewRegistry() }
