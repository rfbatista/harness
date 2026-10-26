package gitcli

import (
	"context"
	"slices"
	"testing"
)

func TestBranchNames_RefFormatProblemFollowsGitsRules(t *testing.T) {
	repo := initRepo(t)
	b := NewBranchNames()
	for name, valid := range map[string]bool{
		"agent/fix-login": true, "feature_1.2": true, "ünïcode": true,
		"": false, "has space": false, "a..b": false, "x.lock": false, "-lead": false,
		"trail/": false, "a//b": false, "end.": false, "a~b": false, "a:b": false, "a@{b": false, "@": false, "@{-1}": false, "refs/heads/x": false,
	} {
		got, err := b.RefFormatProblem(context.Background(), repo, name)
		if err != nil {
			t.Fatal(err)
		}
		if (got == "") != valid {
			t.Errorf("%q: problem %q, valid want %v", name, got, valid)
		}
	}
}

func TestBranchNames_LocalBranchesNamesEveryLocalBranchAndCreatesNothing(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "branch", "agent/x")
	gitIn(t, repo, "branch", "plain")
	got, err := NewBranchNames().LocalBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if want := []string{"agent/x", "main", "plain"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
