package cli

import "testing"

func TestBuildClaudeEnv_ClearsBelayerContextForExplorer(t *testing.T) {
	baseEnv := []string{
		"PATH=/bin",
		"BELAYER_CRAG=old-crag",
		"BELAYER_INSTANCE=old-instance",
		"BELAYER_CRAG=duplicate",
	}

	got := buildClaudeEnv(baseEnv, nil)

	assertEnvContains(t, got, "PATH=/bin")
	assertEnvCount(t, got, "BELAYER_CRAG=", 0)
	assertEnvCount(t, got, "BELAYER_INSTANCE=", 0)
}

func TestBuildClaudeEnv_ReaddsOverriddenCragContextForSetter(t *testing.T) {
	baseEnv := []string{
		"PATH=/bin",
		"BELAYER_CRAG=old-crag",
		"BELAYER_INSTANCE=old-instance",
	}

	got := buildClaudeEnv(baseEnv, map[string]string{"BELAYER_CRAG": "new-crag"})

	assertEnvContains(t, got, "PATH=/bin")
	assertEnvContains(t, got, "BELAYER_CRAG=new-crag")
	assertEnvCount(t, got, "BELAYER_CRAG=", 1)
	assertEnvCount(t, got, "BELAYER_INSTANCE=", 0)
}

func assertEnvContains(t *testing.T, env []string, want string) {
	t.Helper()
	for _, entry := range env {
		if entry == want {
			return
		}
	}
	t.Fatalf("env missing %q: %v", want, env)
}

func assertEnvCount(t *testing.T, env []string, prefix string, want int) {
	t.Helper()
	got := 0
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			got++
		}
	}
	if got != want {
		t.Fatalf("count for %q = %d, want %d in %v", prefix, got, want, env)
	}
}
