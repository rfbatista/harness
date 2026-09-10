package core

import (
	"errors"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

// ErrorClass is how the UI should react to a failed write, decided from the
// stable error code rather than the message text.
type ErrorClass int

const (
	// ErrUnexpected: close what is open, toast the message, log it.
	ErrUnexpected ErrorClass = iota
	// ErrInline: the user can fix it; keep the form open and show the message.
	ErrInline
	// ErrNotFound: the thing is gone; close the drill-in and toast.
	ErrNotFound
	// ErrConflict: a retry with force would succeed; offer it.
	ErrConflict
	// ErrUnavailable: the machine cannot run agents; show a persistent banner.
	ErrUnavailable
)

// Classify maps an error onto an ErrorClass using errs.Code.
func Classify(err error) ErrorClass {
	switch code := errs.Code(err); code {
	case "INVALID_INPUT", "INVALID_NAME", "INVALID_ROOT", "INVALID_PATH", "INVALID_URL",
		"INVALID_PATTERN", "INVALID_STATUS", "NO_ANSWERS", "PUBLISH_ROOT_NOT_SET",
		"BRANCH_EXISTS", "WORKSPACE_EXISTS", "CROSS_PROJECT_ACCESS":
		return ErrInline
	case "PUBLISH_TARGET_EXISTS", "PUBLISH_SLUG_CONFLICT":
		return ErrConflict
	case "CLAUDE_CLI_NOT_FOUND":
		return ErrUnavailable
	}
	if isNotFound(err) {
		return ErrNotFound
	}
	return ErrUnexpected
}

func isNotFound(err error) bool {
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		return false
	}
	const suffix = "_NOT_FOUND"
	return len(se.Code) > len(suffix) && se.Code[len(se.Code)-len(suffix):] == suffix
}

// Message is the human text of an error: the structured message when there is
// one, the plain Error() otherwise.
func Message(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) && se.Message != "" {
		return se.Message
	}
	return err.Error()
}
