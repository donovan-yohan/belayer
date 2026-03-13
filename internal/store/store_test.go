package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsertAndGetProblem(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	problem := &model.Problem{
		ID:         "problem-1",
		CragID: "test-crag",
		Spec:       "# My Spec\nDo the thing.",
		ClimbsJSON: `{"repos":{"api":{"climbs":[{"id":"api-1","description":"Add endpoint","depends_on":[]}]}}}`,
		JiraRef:    "PROJ-123",
		Status:     model.ProblemStatusPending,
	}
	climbs := []model.Climb{
		{ID: "api-1", ProblemID: "problem-1", RepoName: "api", Description: "Add endpoint", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}

	err := s.InsertProblem(problem, climbs)
	require.NoError(t, err)

	got, err := s.GetProblem("problem-1")
	require.NoError(t, err)
	assert.Equal(t, "problem-1", got.ID)
	assert.Equal(t, "test-crag", got.CragID)
	assert.Equal(t, "# My Spec\nDo the thing.", got.Spec)
	assert.Equal(t, "PROJ-123", got.JiraRef)
	assert.Equal(t, model.ProblemStatusPending, got.Status)
}

func TestInsertProblemWithClimbs(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	problem := &model.Problem{
		ID:         "problem-2",
		CragID: "test-crag",
		Spec:       "spec content",
		ClimbsJSON: "{}",
		Status:     model.ProblemStatusPending,
	}
	climbs := []model.Climb{
		{ID: "api-1", ProblemID: "problem-2", RepoName: "api", Description: "First climb", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "api-2", ProblemID: "problem-2", RepoName: "api", Description: "Second climb", DependsOn: []string{"api-1"}, Status: model.ClimbStatusPending},
		{ID: "app-1", ProblemID: "problem-2", RepoName: "app", Description: "App climb", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}

	err := s.InsertProblem(problem, climbs)
	require.NoError(t, err)

	gotClimbs, err := s.GetClimbsForProblem("problem-2")
	require.NoError(t, err)
	assert.Len(t, gotClimbs, 3)

	climbMap := make(map[string]model.Climb)
	for _, c := range gotClimbs {
		climbMap[c.ID] = c
	}

	assert.Equal(t, "api", climbMap["api-1"].RepoName)
	assert.Equal(t, []string{}, climbMap["api-1"].DependsOn)
	assert.Equal(t, []string{"api-1"}, climbMap["api-2"].DependsOn)
	assert.Equal(t, "app", climbMap["app-1"].RepoName)
}

func TestInsertProblemCreatesEvent(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	problem := &model.Problem{
		ID:         "problem-3",
		CragID: "test-crag",
		Spec:       "spec",
		ClimbsJSON: "{}",
		Status:     model.ProblemStatusPending,
	}

	err := s.InsertProblem(problem, nil)
	require.NoError(t, err)

	events, err := s.GetEventsForProblem("problem-3")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, model.EventProblemCreated, events[0].Type)
	assert.Equal(t, "problem-3", events[0].ProblemID)
}

func TestListProblemsForCrag(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	for _, id := range []string{"problem-a", "problem-b"} {
		err := s.InsertProblem(&model.Problem{
			ID: id, CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
		}, nil)
		require.NoError(t, err)
	}

	problems, err := s.ListProblemsForCrag("test-crag")
	require.NoError(t, err)
	assert.Len(t, problems, 2)
}

func TestUpdateProblemStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "problem-4", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	err = s.UpdateProblemStatus("problem-4", model.ProblemStatusRunning)
	require.NoError(t, err)

	got, err := s.GetProblem("problem-4")
	require.NoError(t, err)
	assert.Equal(t, model.ProblemStatusRunning, got.Status)
}

func TestUpdateClimbStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "problem-5", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, []model.Climb{
		{ID: "c-1", ProblemID: "problem-5", RepoName: "api", Description: "climb", DependsOn: []string{}, Status: model.ClimbStatusPending},
	})
	require.NoError(t, err)

	err = s.UpdateClimbStatus("c-1", model.ClimbStatusComplete)
	require.NoError(t, err)

	climbs, err := s.GetClimbsForProblem("problem-5")
	require.NoError(t, err)
	require.Len(t, climbs, 1)
	assert.Equal(t, model.ClimbStatusComplete, climbs[0].Status)
	assert.NotNil(t, climbs[0].CompletedAt)
}

func TestGetProblemNotFound(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	_, err := s.GetProblem("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetProblemsByStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "problem-p", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	err = s.InsertProblem(&model.Problem{
		ID: "problem-r", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)
	err = s.UpdateProblemStatus("problem-r", model.ProblemStatusRunning)
	require.NoError(t, err)

	pending, err := s.GetProblemsByStatus(model.ProblemStatusPending)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, "problem-p", pending[0].ID)

	running, err := s.GetProblemsByStatus(model.ProblemStatusRunning)
	require.NoError(t, err)
	assert.Len(t, running, 1)
	assert.Equal(t, "problem-r", running[0].ID)
}

func TestValidateClimbsFile(t *testing.T) {
	tests := []struct {
		name    string
		cf      model.ClimbsFile
		wantErr string
	}{
		{
			name: "valid single repo",
			cf: model.ClimbsFile{
				Repos: map[string]model.RepoClimbs{
					"api": {Climbs: []model.ClimbSpec{
						{ID: "api-1", Description: "do thing", DependsOn: []string{}},
					}},
				},
			},
		},
		{
			name: "valid with dependencies",
			cf: model.ClimbsFile{
				Repos: map[string]model.RepoClimbs{
					"api": {Climbs: []model.ClimbSpec{
						{ID: "api-1", Description: "first", DependsOn: []string{}},
						{ID: "api-2", Description: "second", DependsOn: []string{"api-1"}},
					}},
				},
			},
		},
		{
			name: "duplicate climb ID",
			cf: model.ClimbsFile{
				Repos: map[string]model.RepoClimbs{
					"api": {Climbs: []model.ClimbSpec{{ID: "c-1", Description: "a"}}},
					"app": {Climbs: []model.ClimbSpec{{ID: "c-1", Description: "b"}}},
				},
			},
			wantErr: "duplicate climb ID",
		},
		{
			name: "empty climb ID",
			cf: model.ClimbsFile{
				Repos: map[string]model.RepoClimbs{
					"api": {Climbs: []model.ClimbSpec{{ID: "", Description: "a"}}},
				},
			},
			wantErr: "empty ID",
		},
		{
			name: "empty description",
			cf: model.ClimbsFile{
				Repos: map[string]model.RepoClimbs{
					"api": {Climbs: []model.ClimbSpec{{ID: "api-1", Description: ""}}},
				},
			},
			wantErr: "empty description",
		},
		{
			name: "depends on nonexistent climb",
			cf: model.ClimbsFile{
				Repos: map[string]model.RepoClimbs{
					"api": {Climbs: []model.ClimbSpec{
						{ID: "api-1", Description: "a", DependsOn: []string{"nope"}},
					}},
				},
			},
			wantErr: "does not exist",
		},
		{
			name: "cross-repo dependency",
			cf: model.ClimbsFile{
				Repos: map[string]model.RepoClimbs{
					"api": {Climbs: []model.ClimbSpec{{ID: "api-1", Description: "a"}}},
					"app": {Climbs: []model.ClimbSpec{{ID: "app-1", Description: "b", DependsOn: []string{"api-1"}}}},
				},
			},
			wantErr: "different repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClimbsFile(&tt.cf)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateClimbsRepos(t *testing.T) {
	cf := &model.ClimbsFile{
		Repos: map[string]model.RepoClimbs{
			"api":     {Climbs: []model.ClimbSpec{{ID: "api-1", Description: "a"}}},
			"unknown": {Climbs: []model.ClimbSpec{{ID: "u-1", Description: "b"}}},
		},
	}

	err := ValidateClimbsRepos(cf, []string{"api", "app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")

	err = ValidateClimbsRepos(cf, []string{"api", "unknown"})
	require.NoError(t, err)
}

func TestClimbsFromFile(t *testing.T) {
	cf := &model.ClimbsFile{
		Repos: map[string]model.RepoClimbs{
			"api": {Climbs: []model.ClimbSpec{
				{ID: "api-1", Description: "first", DependsOn: []string{}},
				{ID: "api-2", Description: "second", DependsOn: []string{"api-1"}},
			}},
			"app": {Climbs: []model.ClimbSpec{
				{ID: "app-1", Description: "app climb"},
			}},
		},
	}

	climbs := ClimbsFromFile("problem-99", cf)
	assert.Len(t, climbs, 3)

	climbMap := make(map[string]model.Climb)
	for _, c := range climbs {
		climbMap[c.ID] = c
	}

	assert.Equal(t, "problem-99", climbMap["api-1"].ProblemID)
	assert.Equal(t, "api", climbMap["api-1"].RepoName)
	assert.Equal(t, []string{}, climbMap["api-1"].DependsOn)
	assert.Equal(t, []string{"api-1"}, climbMap["api-2"].DependsOn)
	assert.Equal(t, []string{}, climbMap["app-1"].DependsOn) // nil converted to empty
	assert.Equal(t, model.ClimbStatusPending, climbMap["app-1"].Status)
}

func TestGetPendingProblems(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	// Insert a pending problem
	err := s.InsertProblem(&model.Problem{
		ID: "problem-pending", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	// Insert a running problem
	err = s.InsertProblem(&model.Problem{
		ID: "problem-running", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)
	err = s.UpdateProblemStatus("problem-running", model.ProblemStatusRunning)
	require.NoError(t, err)

	pending, err := s.GetPendingProblems("test-crag")
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, "problem-pending", pending[0].ID)
	assert.Equal(t, model.ProblemStatusPending, pending[0].Status)
}

func TestGetActiveProblems(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	// Insert problems with various statuses
	err := s.InsertProblem(&model.Problem{
		ID: "problem-p", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	err = s.InsertProblem(&model.Problem{
		ID: "problem-r", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)
	err = s.UpdateProblemStatus("problem-r", model.ProblemStatusRunning)
	require.NoError(t, err)

	err = s.InsertProblem(&model.Problem{
		ID: "problem-rv", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)
	err = s.UpdateProblemStatus("problem-rv", model.ProblemStatusReviewing)
	require.NoError(t, err)

	err = s.InsertProblem(&model.Problem{
		ID: "problem-c", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)
	err = s.UpdateProblemStatus("problem-c", model.ProblemStatusComplete)
	require.NoError(t, err)

	active, err := s.GetActiveProblems("test-crag")
	require.NoError(t, err)
	assert.Len(t, active, 2)

	ids := []string{active[0].ID, active[1].ID}
	assert.Contains(t, ids, "problem-r")
	assert.Contains(t, ids, "problem-rv")
}

func TestIncrementClimbAttempt(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "problem-inc", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, []model.Climb{
		{ID: "c-inc", ProblemID: "problem-inc", RepoName: "api", Description: "climb", DependsOn: []string{}, Status: model.ClimbStatusPending},
	})
	require.NoError(t, err)

	err = s.IncrementClimbAttempt("c-inc")
	require.NoError(t, err)

	climbs, err := s.GetClimbsForProblem("problem-inc")
	require.NoError(t, err)
	require.Len(t, climbs, 1)
	assert.Equal(t, 1, climbs[0].Attempt)
}

func TestResetClimbStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "problem-reset", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, []model.Climb{
		{ID: "c-reset", ProblemID: "problem-reset", RepoName: "api", Description: "climb", DependsOn: []string{}, Status: model.ClimbStatusPending},
	})
	require.NoError(t, err)

	// Mark as complete first
	err = s.UpdateClimbStatus("c-reset", model.ClimbStatusComplete)
	require.NoError(t, err)

	climbs, err := s.GetClimbsForProblem("problem-reset")
	require.NoError(t, err)
	require.Len(t, climbs, 1)
	assert.Equal(t, model.ClimbStatusComplete, climbs[0].Status)
	assert.NotNil(t, climbs[0].CompletedAt)

	// Reset back to pending
	err = s.ResetClimbStatus("c-reset")
	require.NoError(t, err)

	climbs, err = s.GetClimbsForProblem("problem-reset")
	require.NoError(t, err)
	require.Len(t, climbs, 1)
	assert.Equal(t, model.ClimbStatusPending, climbs[0].Status)
	assert.Nil(t, climbs[0].CompletedAt)
}

func TestInsertAndGetAnchorReview(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "problem-sr", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	review := &model.SpotterReview{
		ProblemID: "problem-sr",
		Attempt:   1,
		Verdict:   "pass",
		Output:    "All climbs met.",
	}
	err = s.InsertAnchorReview(review)
	require.NoError(t, err)

	reviews, err := s.GetAnchorReviewsForProblem("problem-sr")
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	assert.Equal(t, "problem-sr", reviews[0].ProblemID)
	assert.Equal(t, 1, reviews[0].Attempt)
	assert.Equal(t, "pass", reviews[0].Verdict)
	assert.Equal(t, "All climbs met.", reviews[0].Output)
	assert.NotZero(t, reviews[0].ID)
	assert.False(t, reviews[0].CreatedAt.IsZero())
}

func TestInsertAndGetTrackerIssue(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	issue := &model.TrackerIssue{
		ID:           "GH-42",
		Provider:     "github",
		Title:        "Fix the bug",
		Body:         "Detailed description",
		CommentsJSON: `[]`,
		LabelsJSON:   `["bug"]`,
		Priority:     "high",
		Assignee:     "alice",
		URL:          "https://github.com/org/repo/issues/42",
		RawJSON:      `{}`,
		ProblemID:    "",
		SyncedAt:     time.Now().UTC(),
	}

	err := s.InsertTrackerIssue(issue)
	require.NoError(t, err)

	got, err := s.GetTrackerIssue("GH-42")
	require.NoError(t, err)
	assert.Equal(t, "GH-42", got.ID)
	assert.Equal(t, "github", got.Provider)
	assert.Equal(t, "Fix the bug", got.Title)
	assert.Equal(t, "high", got.Priority)
	assert.Equal(t, "alice", got.Assignee)
}

func TestGetTrackerIssueNotFound(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	_, err := s.GetTrackerIssue("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListTrackerIssues(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "prob-1", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	issues := []*model.TrackerIssue{
		{ID: "GH-1", Provider: "github", Title: "Linked issue", SyncedAt: now, ProblemID: "prob-1"},
		{ID: "GH-2", Provider: "github", Title: "Unlinked issue", SyncedAt: now, ProblemID: ""},
	}
	for _, ti := range issues {
		require.NoError(t, s.InsertTrackerIssue(ti))
	}

	all, err := s.ListTrackerIssues(false)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	unlinked, err := s.ListTrackerIssues(true)
	require.NoError(t, err)
	require.Len(t, unlinked, 1)
	assert.Equal(t, "GH-2", unlinked[0].ID)
}

func TestLinkTrackerIssueToProblem(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "prob-link", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	issue := &model.TrackerIssue{
		ID: "GH-99", Provider: "github", Title: "Issue to link", SyncedAt: time.Now().UTC(),
	}
	require.NoError(t, s.InsertTrackerIssue(issue))

	err = s.LinkTrackerIssueToProblem("GH-99", "prob-link")
	require.NoError(t, err)

	got, err := s.GetTrackerIssue("GH-99")
	require.NoError(t, err)
	assert.Equal(t, "prob-link", got.ProblemID)
}

func insertTestProblem(t *testing.T, s *Store, id string) {
	t.Helper()
	err := s.InsertProblem(&model.Problem{
		ID: id, CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)
}

func TestInsertAndGetPullRequest(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)
	insertTestProblem(t, s, "prob-pr-1")

	pr := &model.PullRequest{
		ProblemID:    "prob-pr-1",
		RepoName:     "api",
		PRNumber:     42,
		URL:          "https://github.com/org/api/pull/42",
		StackPosition: 1,
		StackSize:    1,
		CIStatus:     "pending",
		CIFixCount:   0,
		ReviewStatus: "pending",
		State:        "open",
	}

	id, err := s.InsertPullRequest(pr)
	require.NoError(t, err)
	assert.Positive(t, id)

	got, err := s.GetPullRequest(id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "prob-pr-1", got.ProblemID)
	assert.Equal(t, "api", got.RepoName)
	assert.Equal(t, 42, got.PRNumber)
	assert.Equal(t, "https://github.com/org/api/pull/42", got.URL)
	assert.Equal(t, "open", got.State)
	assert.Equal(t, "pending", got.CIStatus)
	assert.Nil(t, got.LastPolledAt)
	assert.False(t, got.CreatedAt.IsZero())
}

func TestListPullRequestsForProblem(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)
	insertTestProblem(t, s, "prob-pr-list")

	for i := 1; i <= 3; i++ {
		_, err := s.InsertPullRequest(&model.PullRequest{
			ProblemID: "prob-pr-list", RepoName: "api", PRNumber: i,
			URL: "https://github.com/org/api/pull/" + fmt.Sprintf("%d", i),
			StackPosition: i, StackSize: 3, CIStatus: "pending", ReviewStatus: "pending", State: "open",
		})
		require.NoError(t, err)
	}

	prs, err := s.ListPullRequestsForProblem("prob-pr-list")
	require.NoError(t, err)
	assert.Len(t, prs, 3)
}

func TestUpdatePullRequestCI(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)
	insertTestProblem(t, s, "prob-pr-ci")

	id, err := s.InsertPullRequest(&model.PullRequest{
		ProblemID: "prob-pr-ci", RepoName: "api", PRNumber: 1,
		URL: "https://github.com/org/api/pull/1",
		CIStatus: "pending", ReviewStatus: "pending", State: "open",
	})
	require.NoError(t, err)

	err = s.UpdatePullRequestCI(id, "failed", 2)
	require.NoError(t, err)

	got, err := s.GetPullRequest(id)
	require.NoError(t, err)
	assert.Equal(t, "failed", got.CIStatus)
	assert.Equal(t, 2, got.CIFixCount)
	assert.NotNil(t, got.LastPolledAt)
}

func TestUpdatePullRequestState(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)
	insertTestProblem(t, s, "prob-pr-state")

	id, err := s.InsertPullRequest(&model.PullRequest{
		ProblemID: "prob-pr-state", RepoName: "api", PRNumber: 5,
		URL: "https://github.com/org/api/pull/5",
		CIStatus: "pending", ReviewStatus: "pending", State: "open",
	})
	require.NoError(t, err)

	err = s.UpdatePullRequestState(id, "merged")
	require.NoError(t, err)

	got, err := s.GetPullRequest(id)
	require.NoError(t, err)
	assert.Equal(t, "merged", got.State)
	assert.NotNil(t, got.LastPolledAt)
}

func TestListMonitoredPullRequests(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)
	insertTestProblem(t, s, "prob-monitored")

	openID, err := s.InsertPullRequest(&model.PullRequest{
		ProblemID: "prob-monitored", RepoName: "api", PRNumber: 10,
		URL: "https://github.com/org/api/pull/10",
		CIStatus: "pending", ReviewStatus: "pending", State: "open",
	})
	require.NoError(t, err)

	closedID, err := s.InsertPullRequest(&model.PullRequest{
		ProblemID: "prob-monitored", RepoName: "api", PRNumber: 11,
		URL: "https://github.com/org/api/pull/11",
		CIStatus: "success", ReviewStatus: "approved", State: "merged",
	})
	require.NoError(t, err)
	_ = closedID

	monitored, err := s.ListMonitoredPullRequests("test-crag")
	require.NoError(t, err)
	require.Len(t, monitored, 1)
	assert.Equal(t, openID, monitored[0].ID)
}

func TestInsertAndListPRReactions(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)
	insertTestProblem(t, s, "prob-reaction")

	prID, err := s.InsertPullRequest(&model.PullRequest{
		ProblemID: "prob-reaction", RepoName: "api", PRNumber: 20,
		URL: "https://github.com/org/api/pull/20",
		CIStatus: "pending", ReviewStatus: "pending", State: "open",
	})
	require.NoError(t, err)

	reaction := &model.PRReaction{
		PRID:           prID,
		TriggerType:    "ci_failed",
		TriggerPayload: `{"run_id": "123"}`,
		ActionTaken:    "dispatched_fix",
		LeadID:         "lead-abc",
	}
	err = s.InsertPRReaction(reaction)
	require.NoError(t, err)

	reactions, err := s.ListPRReactions(prID)
	require.NoError(t, err)
	require.Len(t, reactions, 1)
	assert.Equal(t, prID, reactions[0].PRID)
	assert.Equal(t, "ci_failed", reactions[0].TriggerType)
	assert.Equal(t, "dispatched_fix", reactions[0].ActionTaken)
	assert.Equal(t, "lead-abc", reactions[0].LeadID)
	assert.NotZero(t, reactions[0].ID)
	assert.False(t, reactions[0].CreatedAt.IsZero())
}

func TestInsertGetDeleteEnvironment(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)
	insertTestProblem(t, s, "prob-env-1")

	err := s.InsertEnvironment("prob-env-1", "myenv start", "dev", `{"FOO":"bar"}`)
	require.NoError(t, err)

	got, err := s.GetEnvironment("prob-env-1")
	require.NoError(t, err)
	assert.Equal(t, "prob-env-1", got.ProblemID)
	assert.Equal(t, "myenv start", got.ProviderCommand)
	assert.Equal(t, "dev", got.EnvName)
	assert.Equal(t, `{"FOO":"bar"}`, got.EnvJSON)
	assert.False(t, got.CreatedAt.IsZero())

	err = s.DeleteEnvironment("prob-env-1")
	require.NoError(t, err)

	_, err = s.GetEnvironment("prob-env-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetEnvironmentNotFound(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	_, err := s.GetEnvironment("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateAndGetClimbWorktreePath(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "prob-wt", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, []model.Climb{
		{ID: "c-wt", ProblemID: "prob-wt", RepoName: "api", Description: "climb", DependsOn: []string{}, Status: model.ClimbStatusPending},
	})
	require.NoError(t, err)

	path, err := s.GetClimbWorktreePath("c-wt")
	require.NoError(t, err)
	assert.Equal(t, "", path)

	err = s.UpdateClimbWorktreePath("c-wt", "/tmp/worktrees/c-wt")
	require.NoError(t, err)

	path, err = s.GetClimbWorktreePath("c-wt")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktrees/c-wt", path)
}

func TestGetClimbWorktreePathNotFound(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	_, err := s.GetClimbWorktreePath("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestInsertClimbs(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertProblem(&model.Problem{
		ID: "problem-ic", CragID: "test-crag", Spec: "s", ClimbsJSON: "{}", Status: model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	correctionClimbs := []model.Climb{
		{ID: "cc-1", ProblemID: "problem-ic", RepoName: "api", Description: "correction climb 1", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "cc-2", ProblemID: "problem-ic", RepoName: "app", Description: "correction climb 2", DependsOn: []string{"cc-1"}, Status: model.ClimbStatusPending},
	}

	err = s.InsertClimbs(correctionClimbs)
	require.NoError(t, err)

	climbs, err := s.GetClimbsForProblem("problem-ic")
	require.NoError(t, err)
	assert.Len(t, climbs, 2)

	climbMap := make(map[string]model.Climb)
	for _, c := range climbs {
		climbMap[c.ID] = c
	}

	assert.Equal(t, "api", climbMap["cc-1"].RepoName)
	assert.Equal(t, "correction climb 1", climbMap["cc-1"].Description)
	assert.Equal(t, []string{}, climbMap["cc-1"].DependsOn)
	assert.Equal(t, model.ClimbStatusPending, climbMap["cc-1"].Status)
	assert.Equal(t, 0, climbMap["cc-1"].Attempt)

	assert.Equal(t, "app", climbMap["cc-2"].RepoName)
	assert.Equal(t, []string{"cc-1"}, climbMap["cc-2"].DependsOn)
}
