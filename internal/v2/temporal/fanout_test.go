package temporal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopoSort_NoDependencies(t *testing.T) {
	repos := map[string]RepoTask{
		"a": {Needed: true, Spec: "task a"},
		"b": {Needed: true, Spec: "task b"},
		"c": {Needed: true, Spec: "task c"},
	}
	levels, err := topoSort(repos)
	require.NoError(t, err)
	assert.Len(t, levels, 1)
	assert.Len(t, levels[0], 3)
}

func TestTopoSort_LinearDependency(t *testing.T) {
	repos := map[string]RepoTask{
		"api":     {Needed: true, Spec: "api", DependsOn: []string{}},
		"app":     {Needed: true, Spec: "app", DependsOn: []string{"api"}},
		"android": {Needed: true, Spec: "android", DependsOn: []string{"app"}},
	}
	levels, err := topoSort(repos)
	require.NoError(t, err)
	assert.Len(t, levels, 3)
	assert.Equal(t, []string{"api"}, levels[0])
	assert.Equal(t, []string{"app"}, levels[1])
	assert.Equal(t, []string{"android"}, levels[2])
}

func TestTopoSort_DiamondDependency(t *testing.T) {
	repos := map[string]RepoTask{
		"api":     {Needed: true, Spec: "api"},
		"app":     {Needed: true, Spec: "app", DependsOn: []string{"api"}},
		"android": {Needed: true, Spec: "android", DependsOn: []string{"api"}},
		"e2e":     {Needed: true, Spec: "e2e", DependsOn: []string{"app", "android"}},
	}
	levels, err := topoSort(repos)
	require.NoError(t, err)
	assert.Len(t, levels, 3)
	assert.Equal(t, []string{"api"}, levels[0])
	assert.ElementsMatch(t, []string{"android", "app"}, levels[1])
	assert.Equal(t, []string{"e2e"}, levels[2])
}

func TestTopoSort_CircularDependency(t *testing.T) {
	repos := map[string]RepoTask{
		"a": {Needed: true, Spec: "a", DependsOn: []string{"b"}},
		"b": {Needed: true, Spec: "b", DependsOn: []string{"a"}},
	}
	_, err := topoSort(repos)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestTopoSort_DepOnSkippedRepo(t *testing.T) {
	// "app" depends on "api" but "api" is not in the needed set.
	repos := map[string]RepoTask{
		"app": {Needed: true, Spec: "app", DependsOn: []string{"api"}},
	}
	levels, err := topoSort(repos)
	require.NoError(t, err)
	assert.Len(t, levels, 1)
	assert.Equal(t, []string{"app"}, levels[0])
}

func TestFanOutResult_SkippedRepos(t *testing.T) {
	output := DecomposerOutput{
		Repos: map[string]RepoTask{
			"api": {Needed: true, Spec: "build api"},
			"ios": {Needed: false, Reason: "no iOS changes needed"},
		},
	}

	// Verify needed vs skipped partitioning.
	needed := 0
	skipped := 0
	for _, task := range output.Repos {
		if task.Needed {
			needed++
		} else {
			skipped++
		}
	}
	assert.Equal(t, 1, needed)
	assert.Equal(t, 1, skipped)
}

func TestDecomposerOutput_AllUnneeded(t *testing.T) {
	output := DecomposerOutput{
		Repos: map[string]RepoTask{
			"api": {Needed: false, Reason: "no api changes"},
			"app": {Needed: false, Reason: "no app changes"},
		},
	}

	needed := 0
	for _, task := range output.Repos {
		if task.Needed {
			needed++
		}
	}
	assert.Equal(t, 0, needed)
}
