package domain

// StructuredError is implemented by github.com/rfbatista/harnesskit/errs.
//
// This is a type alias, not a new type: *domain.StructuredError and
// *errs.StructuredError are the same type, so the ~320 construction sites in
// this repo, and every errors.As against them, keep working unchanged.
//
// Prefer errs.Code(err) over hand-rolling errors.As when you only need the code.

import "github.com/rfbatista/harnesskit/errs"

// StructuredError pairs a stable, machine-readable Code with a human message.
// It is the domain error type used across application and adapters.
type StructuredError = errs.StructuredError
