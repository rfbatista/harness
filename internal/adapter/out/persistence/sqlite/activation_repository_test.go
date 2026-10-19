package sqlite

import (
	"context"
	"testing"

	"operators-mcp/internal/domain"
)

// The activations of many owners of one level come in one read, and only
// theirs.
func TestActivationRepository_ListOwnersReadsTheNamedOwnersOfALevel(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewActivationRepository(db)
	repo := func(id string) domain.ActivationOwner {
		return domain.ActivationOwner{Level: domain.ActivationLevelRepository, ID: id}
	}
	saver := domain.TokenSavers[0]
	for _, a := range []domain.Activation{
		{Saver: saver, Owner: repo("r1"), ProjectID: "p1", Value: domain.ActivationOn},
		{Saver: saver, Owner: repo("r2"), ProjectID: "p1", Value: domain.ActivationOff},
		{Saver: saver, Owner: repo("r3"), ProjectID: "p1", Value: domain.ActivationOn},
		{Saver: saver, Owner: domain.ActivationOwner{Level: domain.ActivationLevelProject, ID: "r1"}, ProjectID: "p1", Value: domain.ActivationOff},
	} {
		if err := r.Put(a); err != nil {
			t.Fatal(err)
		}
	}

	got, err := r.ListOwners(context.Background(), domain.ActivationLevelRepository, []string{"r1", "r2"})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]domain.ActivationValue{}
	for _, a := range got {
		if a.Owner.Level != domain.ActivationLevelRepository {
			t.Errorf("got an activation of level %s", a.Owner.Level)
		}
		values[a.Owner.ID] = a.Value
	}
	if len(got) != 2 || values["r1"] != domain.ActivationOn || values["r2"] != domain.ActivationOff {
		t.Errorf("ListOwners(r1, r2) = %+v, want r1 on and r2 off", got)
	}
	if none, err := r.ListOwners(context.Background(), domain.ActivationLevelRepository, nil); err != nil || len(none) != 0 {
		t.Errorf("ListOwners(none) = %v, %v; want nothing", none, err)
	}
}
