package sqlite

import (
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

func TestSkillRepo_FilesRoundTrip(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSkillRepository(db)

	created, err := repo.Create(domain.SkillInput{
		Name: "s",
		Files: []domain.SkillFile{
			{Path: "SKILL.md", Content: "---\nname: s\ndescription: d\n---\n\nb"},
			{Path: "scripts/run.py", Content: "print(1)"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := repo.Get(created.ID)
	if got == nil || len(got.Files) != 2 {
		t.Fatalf("expected 2 files, got %+v", got)
	}

	// per-file put + prefix delete
	if err := repo.PutFile(created.ID, domain.SkillFile{Path: "scripts/util.py", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteFile(created.ID, "scripts"); err != nil {
		t.Fatal(err)
	}
	files, _ := repo.ListFiles(created.ID)
	if len(files) != 1 || files[0].Path != "SKILL.md" {
		t.Fatalf("prefix delete failed: %+v", files)
	}
}

func TestMigrateSkillsToFiles_InlineContent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// insert a legacy inline skill directly (no file rows)
	legacy := &SkillModel{
		ID:          "legacy1",
		Name:        "legacy",
		Description: "old inline skill",
		Content:     "Do the thing.",
		Metadata:    stringMap{},
	}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateSkillsToFiles(db); err != nil {
		t.Fatal(err)
	}

	repo := NewSkillRepository(db)
	got := repo.Get("legacy1")
	if got == nil || len(got.Files) != 1 || got.Files[0].Path != domain.SkillFileName {
		t.Fatalf("inline skill not migrated to SKILL.md: %+v", got)
	}

	// idempotent: re-running does not duplicate rows
	if err := migrateSkillsToFiles(db); err != nil {
		t.Fatal(err)
	}
	files, _ := repo.ListFiles("legacy1")
	if len(files) != 1 {
		t.Fatalf("migration not idempotent: %+v", files)
	}
}

func TestSkillRepository_PublishState(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSkillRepository(db)
	created, err := repo.Create(domain.SkillInput{
		Name:  "code review",
		Files: []domain.SkillFile{{Path: "SKILL.md", Content: "# hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.IsPublished() {
		t.Fatal("a new skill must not be published")
	}

	at := time.Now().UTC().Truncate(time.Second)
	if err := repo.SetPublishState(created.ID, domain.SkillPublishState{Slug: "code-review", At: at}); err != nil {
		t.Fatal(err)
	}
	got := repo.Get(created.ID)
	if got.PublishedSlug != "code-review" || !got.IsPublished() {
		t.Fatalf("published slug not persisted: %+v", got)
	}
	if got.PublishedAt == nil || !got.PublishedAt.Equal(at) {
		t.Fatalf("published_at = %v, want %v", got.PublishedAt, at)
	}
	if got.PublishError != "" {
		t.Fatalf("publish error should be empty, got %q", got.PublishError)
	}

	// A failed sync records the message without losing the slug.
	if err := repo.SetPublishState(created.ID, domain.SkillPublishState{Slug: "code-review", At: at, Error: "disk full"}); err != nil {
		t.Fatal(err)
	}
	if got = repo.Get(created.ID); got.PublishError != "disk full" || got.PublishedSlug != "code-review" {
		t.Fatalf("error state wrong: %+v", got)
	}

	// A metadata update must not clear the publish state.
	if _, err := repo.Update(created.ID, domain.SkillInput{Name: "code review", Description: "d", Files: got.Files}); err != nil {
		t.Fatal(err)
	}
	if got = repo.Get(created.ID); got.PublishedSlug != "code-review" {
		t.Fatalf("update cleared publish state: %+v", got)
	}

	// The zero state unpublishes.
	if err := repo.SetPublishState(created.ID, domain.SkillPublishState{}); err != nil {
		t.Fatal(err)
	}
	if got = repo.Get(created.ID); got.IsPublished() || got.PublishedAt != nil || got.PublishError != "" {
		t.Fatalf("expected an unpublished skill, got %+v", got)
	}

	if err := repo.SetPublishState("missing", domain.SkillPublishState{}); err == nil {
		t.Fatal("expected SKILL_NOT_FOUND for an unknown skill")
	}
}
