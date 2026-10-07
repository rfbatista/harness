package orchestration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeArtifactReader lists a fixed set of artifacts by task, the way the
// artifacts service does: produced or attached.
type fakeArtifactReader struct{ list []*domain.Artifact }

func (f fakeArtifactReader) ListArtifacts(_ context.Context, flt ports.ArtifactFilter) ([]*domain.Artifact, error) {
	var out []*domain.Artifact
	for _, a := range f.list {
		if a.TicketID == flt.TicketID || a.AttachedTo(flt.TicketID) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (fakeArtifactReader) GetArtifact(context.Context, string) (*domain.Artifact, error) {
	return nil, nil
}
func (fakeArtifactReader) DeleteArtifact(context.Context, string) error { return nil }
func (fakeArtifactReader) OpenArtifactFile(context.Context, string, string) (*ports.ArtifactFile, error) {
	return nil, nil
}

// The brief names the attach tools and says list_task_artifacts includes
// attached assets.
func TestTaskBrief_NamesTheAttachTools(t *testing.T) {
	got := taskBrief(&domain.Ticket{ID: "tk1", Title: "Ship the thing"}, domain.RolePeer)
	for _, want := range []string{
		"\n- mcp__task__attach_artifact_to_task — ",
		"\n- mcp__task__detach_artifact_from_task — ",
		"attached to this task",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
}

// The attached-assets section lists each asset once, says which task made
// it, and stops at 20.
func TestAttachedAssetsSection(t *testing.T) {
	if got := attachedAssetsSection(nil); got != "" {
		t.Fatalf("no assets: %q, want nothing", got)
	}
	got := attachedAssetsSection([]*domain.Artifact{
		{ID: "a1", TicketID: "tk9", Title: "Logo", Kind: domain.ArtifactPage},
	})
	for _, want := range []string{"## Project assets attached to this task", "\n- a1 — Logo (page), from task tk9", "list_task_artifacts"} {
		if !strings.Contains(got, want) {
			t.Errorf("section lacks %q:\n%s", want, got)
		}
	}
	var many []*domain.Artifact
	for i := 0; i < 23; i++ {
		many = append(many, &domain.Artifact{ID: fmt.Sprintf("a%d", i), TicketID: "tk9", Title: "Asset", Kind: domain.ArtifactImage})
	}
	got = attachedAssetsSection(many)
	if n := strings.Count(got, "\n- a"); n != 20 || !strings.Contains(got, "+3 more") {
		t.Fatalf("%d lines listed:\n%s", n, got)
	}
}

// A session starting on a task is told the project assets attached to it,
// and not the artifacts the task produced itself.
func TestStartInteractive_BriefsTheAttachedAssets(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Artifacts = fakeArtifactReader{list: []*domain.Artifact{
		{ID: "a-logo", TicketID: "tk9", Title: "Logo", Kind: domain.ArtifactPage, Scope: domain.ArtifactScopeProject, AttachedTicketIDs: []string{"tk1"}},
		{ID: "a-own", TicketID: "tk1", Title: "Own draft", Kind: domain.ArtifactImage},
	}}
	_, launch := startInteractive(t, svc, InteractiveRequest{})
	brief := briefOf(t, launch)
	if !strings.Contains(brief, "\n- a-logo — Logo (page), from task tk9") {
		t.Fatalf("brief does not list the attached logo:\n%s", brief)
	}
	if strings.Contains(brief, "a-own") {
		t.Fatalf("the task's own artifact is listed as attached:\n%s", brief)
	}

	svc.Artifacts = fakeArtifactReader{}
	_, launch = startInteractive(t, svc, InteractiveRequest{})
	if strings.Contains(briefOf(t, launch), "Project assets attached") {
		t.Fatal("a task with nothing attached gets the section")
	}
}
