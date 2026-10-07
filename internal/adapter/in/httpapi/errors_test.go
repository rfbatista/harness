package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

func TestErrorHandler_ArtifactScopeCodes(t *testing.T) {
	for code, want := range map[string]int{"ARTIFACT_NOT_PROMOTABLE": http.StatusBadRequest, "ARTIFACT_IN_PROJECT": http.StatusConflict} {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)
		errorHandler(&domain.StructuredError{Code: code, Message: "m"}, c)
		if rec.Code != want || !strings.Contains(rec.Body.String(), `"code":"`+code+`"`) {
			t.Errorf("%s = %d %s, want %d", code, rec.Code, rec.Body, want)
		}
	}
}

// The codes a resume answers, at the statuses the session resume contract
// fixes.
func TestErrorHandler_ResumeCodes(t *testing.T) {
	for code, want := range map[string]int{
		"SESSION_NOT_FOUND":          http.StatusNotFound,
		"SESSION_NOT_INTERACTIVE":    http.StatusConflict,
		"SESSION_ALREADY_RUNNING":    http.StatusConflict,
		"WORKSPACE_MISSING":          http.StatusConflict,
		"SESSION_TRANSCRIPT_MISSING": http.StatusConflict,
		"INVALID_INPUT":              http.StatusBadRequest,
	} {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)
		errorHandler(&domain.StructuredError{Code: code, Message: "m"}, c)
		if rec.Code != want || !strings.Contains(rec.Body.String(), `"code":"`+code+`"`) {
			t.Errorf("%s = %d %s, want %d", code, rec.Code, rec.Body, want)
		}
	}
}
