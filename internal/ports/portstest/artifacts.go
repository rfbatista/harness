package portstest

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// AttachmentFixture is what ArtifactAttachmentsConformance starts from: a
// project asset produced by task Producer, another task Other of the same
// project, a task Foreign of another project, and a task-scoped artifact
// TaskArtifactID.
type AttachmentFixture struct {
	AssetID        string
	Producer       string
	Other          string
	Foreign        string
	TaskArtifactID string
}

// ArtifactAttachmentsConformance runs the attachment contract against a
// fresh port and fixture from newPort.
func ArtifactAttachmentsConformance(t *testing.T, newPort func(t *testing.T) (ports.ArtifactAttachments, AttachmentFixture)) {
	ctx := context.Background()

	t.Run("attach and detach round trip", func(t *testing.T) {
		p, f := newPort(t)
		a, err := p.AttachArtifactToTicket(ctx, f.AssetID, f.Other)
		if err != nil {
			t.Fatal(err)
		}
		if a.ID != f.AssetID || strings.Join(a.AttachedTicketIDs, ",") != f.Other {
			t.Fatalf("attach = %+v", a)
		}
		again, err := p.AttachArtifactToTicket(ctx, f.AssetID, f.Other)
		if err != nil || strings.Join(again.AttachedTicketIDs, ",") != f.Other {
			t.Fatalf("attach again = %+v %v", again, err)
		}
		producer, err := p.AttachArtifactToTicket(ctx, f.AssetID, f.Producer)
		if err != nil || strings.Join(producer.AttachedTicketIDs, ",") != f.Other {
			t.Fatalf("attach to the producer = %+v %v", producer, err)
		}
		d, err := p.DetachArtifactFromTicket(ctx, f.AssetID, f.Other)
		if err != nil || d.AttachedTicketIDs == nil || len(d.AttachedTicketIDs) != 0 {
			t.Fatalf("detach = %+v %v", d, err)
		}
		if _, err := p.DetachArtifactFromTicket(ctx, f.AssetID, f.Other); err != nil {
			t.Fatalf("detach again: %v", err)
		}
	})

	t.Run("error codes", func(t *testing.T) {
		p, f := newPort(t)
		_, err := p.AttachArtifactToTicket(ctx, "", f.Other)
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.DetachArtifactFromTicket(ctx, f.AssetID, "")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.AttachArtifactToTicket(ctx, "missing", f.Other)
		wantCode(t, err, "ARTIFACT_NOT_FOUND")
		_, err = p.AttachArtifactToTicket(ctx, f.AssetID, "missing")
		wantCode(t, err, "TICKET_NOT_FOUND")
		_, err = p.AttachArtifactToTicket(ctx, f.AssetID, f.Foreign)
		wantCode(t, err, "ARTIFACT_PROJECT_MISMATCH")
		_, err = p.AttachArtifactToTicket(ctx, f.TaskArtifactID, f.Other)
		wantCode(t, err, "ARTIFACT_NOT_IN_PROJECT")
		_, err = p.DetachArtifactFromTicket(ctx, f.AssetID, f.Producer)
		wantCode(t, err, "ARTIFACT_PRODUCER_TASK")
	})
}
