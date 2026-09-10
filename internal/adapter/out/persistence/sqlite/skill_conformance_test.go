package sqlite

import (
	"testing"

	"github.com/rfbatista/harnesskit/skill"
	"github.com/rfbatista/harnesskit/skill/skilltest"
)

// The SQLite repository has to be interchangeable with harnesskit's in-memory
// store, since the application service is written against the port and not
// against either implementation. This runs the library's own contract suite
// against the real database.
func TestSkillRepository_StoreConformance(t *testing.T) {
	skilltest.StoreConformance(t, func(t *testing.T) skill.Store {
		db, err := Open(":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		return NewSkillRepository(db)
	})
}
