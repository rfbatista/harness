package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"operators-mcp/internal/application/artifacts/artifactstest"
	"operators-mcp/internal/domain"
)

type attachResp struct {
	Artifact *domain.Artifact `json:"artifact"`
	Code     string           `json:"code"`
}

func postAttach(t *testing.T, url, body string) (int, attachResp) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out attachResp
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// The attach and detach routes answer with the stored artifact, and each
// refusal with its status and stable code.
func TestHTTP_AttachDetachArtifact(t *testing.T) {
	svc, f := artifactstest.New(t)
	srv := httptest.NewServer(NewRouter(&Handler{artifactsSvc: svc}))
	defer srv.Close()
	attach, detach := srv.URL+"/api/attach_artifact_to_ticket", srv.URL+"/api/detach_artifact_from_ticket"
	body := func(artifact, ticket string) string {
		b, _ := json.Marshal(map[string]string{"artifact_id": artifact, "ticket_id": ticket})
		return string(b)
	}

	status, out := postAttach(t, attach, body(f.AssetID, f.Other))
	if status != 200 || out.Artifact == nil || strings.Join(out.Artifact.AttachedTicketIDs, ",") != f.Other {
		t.Fatalf("attach = %d %+v", status, out)
	}

	resp := get(t, srv.URL+"/api/artifacts?ticket_id="+f.Other, nil)
	var listed struct {
		Artifacts []domain.Artifact `json:"artifacts"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if len(listed.Artifacts) != 1 || listed.Artifacts[0].ID != f.AssetID {
		t.Fatalf("the attached task lists %+v", listed.Artifacts)
	}

	for _, c := range []struct {
		name, url, body, code string
		status                int
	}{
		{"missing ticket_id", attach, body(f.AssetID, ""), "INVALID_INPUT", 400},
		{"no such artifact", attach, body("missing", f.Other), "ARTIFACT_NOT_FOUND", 404},
		{"no such task", attach, body(f.AssetID, "missing"), "TICKET_NOT_FOUND", 404},
		{"task of another project", attach, body(f.AssetID, f.Foreign), "ARTIFACT_PROJECT_MISMATCH", 409},
		{"task artifact", attach, body(f.TaskArtifactID, f.Other), "ARTIFACT_NOT_IN_PROJECT", 409},
		{"producing task", detach, body(f.AssetID, f.Producer), "ARTIFACT_PRODUCER_TASK", 409},
	} {
		t.Run(c.name, func(t *testing.T) {
			status, out := postAttach(t, c.url, c.body)
			if status != c.status || out.Code != c.code {
				t.Fatalf("= %d %q, want %d %s", status, out.Code, c.status, c.code)
			}
		})
	}

	status, out = postAttach(t, detach, body(f.AssetID, f.Other))
	if status != 200 || out.Artifact == nil || len(out.Artifact.AttachedTicketIDs) != 0 {
		t.Fatalf("detach = %d %+v", status, out)
	}
}
