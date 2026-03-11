package scm

import (
	"testing"
)

func TestCalculateStackSplits_UnderThreshold(t *testing.T) {
	climbs := []ClimbDiff{
		{ClimbID: "c1", RepoName: "repo1", LinesChanged: 50, Commits: []string{"abc1", "abc2"}},
		{ClimbID: "c2", RepoName: "repo2", LinesChanged: 30, Commits: []string{"def1"}},
	}
	splits := CalculateStackSplits(climbs, 200)
	if len(splits) != 1 {
		t.Fatalf("expected 1 split, got %d", len(splits))
	}
	if splits[0].StackPosition != 1 {
		t.Errorf("expected StackPosition=1, got %d", splits[0].StackPosition)
	}
	if len(splits[0].Commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(splits[0].Commits))
	}
}

// TestCalculateStackSplits_OverThreshold verifies climbs are split into multiple PRs.
// Two climbs of 80 lines each with threshold=100 → each climb gets its own PR.
func TestCalculateStackSplits_OverThreshold(t *testing.T) {
	climbs := []ClimbDiff{
		{ClimbID: "c1", RepoName: "repo1", LinesChanged: 80, Commits: []string{"abc1", "abc2"}},
		{ClimbID: "c2", RepoName: "repo2", LinesChanged: 80, Commits: []string{"def1", "def2"}},
	}
	splits := CalculateStackSplits(climbs, 100)
	if len(splits) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(splits))
	}
	if splits[0].StackPosition != 1 {
		t.Errorf("expected split[0].StackPosition=1, got %d", splits[0].StackPosition)
	}
	if splits[1].StackPosition != 2 {
		t.Errorf("expected split[1].StackPosition=2, got %d", splits[1].StackPosition)
	}
	if len(splits[0].Commits) != 2 {
		t.Errorf("expected 2 commits in split[0], got %d", len(splits[0].Commits))
	}
	if len(splits[1].Commits) != 2 {
		t.Errorf("expected 2 commits in split[1], got %d", len(splits[1].Commits))
	}
}

// TestCalculateStackSplits_SingleLargeClimb verifies a single climb exceeding threshold
// still gets placed into its own PR (currentSize==0 guard prevents infinite loop).
func TestCalculateStackSplits_SingleLargeClimb(t *testing.T) {
	climbs := []ClimbDiff{
		{ClimbID: "c1", RepoName: "repo1", LinesChanged: 500, Commits: []string{"abc1", "abc2", "abc3"}},
	}
	splits := CalculateStackSplits(climbs, 100)
	if len(splits) != 1 {
		t.Fatalf("expected 1 split, got %d", len(splits))
	}
	if splits[0].StackPosition != 1 {
		t.Errorf("expected StackPosition=1, got %d", splits[0].StackPosition)
	}
	if len(splits[0].Commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(splits[0].Commits))
	}
}

// TestCalculateStackSplits_ExactThreshold verifies total == threshold → single PR.
func TestCalculateStackSplits_ExactThreshold(t *testing.T) {
	climbs := []ClimbDiff{
		{ClimbID: "c1", RepoName: "repo1", LinesChanged: 50, Commits: []string{"abc1"}},
		{ClimbID: "c2", RepoName: "repo2", LinesChanged: 50, Commits: []string{"def1"}},
	}
	splits := CalculateStackSplits(climbs, 100)
	if len(splits) != 1 {
		t.Fatalf("expected 1 split (total == threshold), got %d", len(splits))
	}
	if splits[0].StackPosition != 1 {
		t.Errorf("expected StackPosition=1, got %d", splits[0].StackPosition)
	}
	if len(splits[0].Commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(splits[0].Commits))
	}
}
