package agentsystem_test

import (
	"context"
	"errors"
	"testing"

	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/domain"
)

func TestCheckShImportWithoutADetectorSaysSoInsteadOfAnsweringNothingToImport(t *testing.T) {
	svc := agentsystem.New(world()) // no Detector

	_, err := svc.CheckShImport(context.Background(), "p1", "r1")

	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "DETECTION_UNAVAILABLE" {
		t.Fatalf("err = %v, want DETECTION_UNAVAILABLE", err)
	}
}
