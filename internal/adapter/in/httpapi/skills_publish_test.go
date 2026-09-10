package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rfbatista/harnesskit/skillfs"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/domain"
)

// newPublishHandler builds a handler whose service has publishing wired, plus
// the service itself for arranging fixtures directly. The agent repository is
// real because DeleteSkill unlinks skills from agents.
func newPublishHandler(t *testing.T) (*Handler, *blueprint.Service) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := blueprint.NewService(
		nil, nil, nil,
		sqlite.NewAgentRepository(db),
		nil,
		sqlite.NewSkillRepository(db),
		nil, nil, nil, nil, "",
	).WithPublishing(sqlite.NewSettingsRepository(db), skillfs.NewPublisher())
	return NewHandler(svc, nil, nil, nil, nil, nil), svc
}

func postJSON(t *testing.T, h *Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api"+path, bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	return rec
}

func getJSON(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api"+path, nil)
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	return rec
}

func newPublishTestSkill(t *testing.T, svc *blueprint.Service) *domain.Skill {
	t.Helper()
	skill, err := svc.CreateSkill(domain.SkillInput{
		Name:  "demo",
		Files: []domain.SkillFile{{Path: "SKILL.md", Content: "# demo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return skill
}

func TestHTTP_PublishAndUnpublishSkill(t *testing.T) {
	h, svc := newPublishHandler(t)
	root := t.TempDir()

	rec := postJSON(t, h, "/update_settings", map[string]any{
		"settings": map[string]string{domain.SettingSkillsPublishRoot: root},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update_settings status = %d body=%s", rec.Code, rec.Body.String())
	}

	skill := newPublishTestSkill(t, svc)

	rec = postJSON(t, h, "/publish_skill", map[string]any{"skill_id": skill.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("publish status = %d body=%s", rec.Code, rec.Body.String())
	}
	published := decodeSkill(t, rec.Body.Bytes())
	if published["published_path"] == nil || published["published_at"] == nil {
		t.Fatalf("published fields missing: %v", published)
	}

	rec = postJSON(t, h, "/unpublish_skill", map[string]any{"skill_id": skill.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("unpublish status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeSkill(t, rec.Body.Bytes()); got["published_path"] != nil {
		t.Fatalf("published_path should be omitted after unpublish: %v", got)
	}
}

func TestHTTP_PublishSkill_RootNotSet(t *testing.T) {
	h, svc := newPublishHandler(t)
	skill := newPublishTestSkill(t, svc)

	rec := postJSON(t, h, "/publish_skill", map[string]any{"skill_id": skill.ID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != "PUBLISH_ROOT_NOT_SET" {
		t.Fatalf("code = %q", payload["code"])
	}
}

func TestHTTP_PublishSkill_TargetExistsIsConflict(t *testing.T) {
	h, svc := newPublishHandler(t)
	root := t.TempDir()
	if _, err := svc.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: root}); err != nil {
		t.Fatal(err)
	}
	skill := newPublishTestSkill(t, svc)
	// A folder the app did not create.
	if _, err := skillfs.NewPublisher().Publish(root, "demo", []domain.SkillFile{{Path: "SKILL.md", Content: "x"}}, false); err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, h, "/publish_skill", map[string]any{"skill_id": skill.ID})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = postJSON(t, h, "/publish_skill", map[string]any{"skill_id": skill.ID, "force": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("forced publish status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_GetSettings(t *testing.T) {
	h, _ := newPublishHandler(t)

	rec := getJSON(t, h, "/get_settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Settings == nil {
		t.Fatal("settings should be an object, not null")
	}
}
