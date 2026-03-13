package envprovider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeScript writes a bash script to a temp dir and returns its path.
// The script echoes a JSON payload when the action matches and exits 0;
// for any action starting with "fail" it echoes an error JSON and exits 1.
func makeScript(t *testing.T, responses map[string]any) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "mockprovider")

	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\n")
	sb.WriteString(`action="$2"` + "\n") // argv: subcommand action [flags...] --json
	sb.WriteString("case \"$action\" in\n")

	for action, payload := range responses {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal mock payload for %q: %v", action, err)
		}
		if strings.HasPrefix(action, "fail:") {
			realAction := strings.TrimPrefix(action, "fail:")
			sb.WriteString("  " + realAction + ")\n")
			sb.WriteString("    echo '" + string(data) + "'\n")
			sb.WriteString("    exit 1\n")
			sb.WriteString("    ;;\n")
		} else {
			sb.WriteString("  " + action + ")\n")
			sb.WriteString("    echo '" + string(data) + "'\n")
			sb.WriteString("    exit 0\n")
			sb.WriteString("    ;;\n")
		}
	}
	sb.WriteString("  *)\n")
	sb.WriteString("    echo '{\"status\":\"error\",\"error\":\"unknown action\",\"code\":\"UNKNOWN\"}'\n")
	sb.WriteString("    exit 1\n")
	sb.WriteString("    ;;\n")
	sb.WriteString("esac\n")

	if err := os.WriteFile(path, []byte(sb.String()), 0o755); err != nil {
		t.Fatalf("write mock script: %v", err)
	}
	return path
}

func TestClient_CreateEnv(t *testing.T) {
	script := makeScript(t, map[string]any{
		"create": CreateEnvResponse{
			Status: "ok",
			Name:   "dev-1",
			Index:  1,
			Env:    map[string]string{"PORT": "8080"},
			Services: map[string]ServiceStatus{
				"api": {Status: "running", Port: 8080, Uptime: "0s"},
			},
		},
	})

	c := NewClient(script, "env")
	resp, err := c.CreateEnv(context.Background(), "dev-1", "")
	if err != nil {
		t.Fatalf("CreateEnv: %v", err)
	}
	if resp.Name != "dev-1" || resp.Index != 1 {
		t.Errorf("unexpected response: %+v", resp)
	}
	if resp.Env["PORT"] != "8080" {
		t.Errorf("env mismatch: %v", resp.Env)
	}
	if svc, ok := resp.Services["api"]; !ok || svc.Port != 8080 {
		t.Errorf("services mismatch: %v", resp.Services)
	}
}

func TestClient_CreateEnv_WithSnapshot(t *testing.T) {
	// Verify --snapshot flag is passed by checking argv in the script.
	dir := t.TempDir()
	script := filepath.Join(dir, "mockprovider")
	src := `#!/usr/bin/env bash
# check that --snapshot arg is present
found=0
for arg in "$@"; do
  if [ "$arg" = "--snapshot" ]; then found=1; fi
done
if [ "$found" = "0" ]; then
  echo '{"status":"error","error":"missing --snapshot","code":"BAD_ARGS"}'
  exit 1
fi
echo '{"status":"ok","name":"dev-snap","index":2}'
exit 0
`
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	c := NewClient(script, "env")
	resp, err := c.CreateEnv(context.Background(), "dev-snap", "snap-1")
	if err != nil {
		t.Fatalf("CreateEnv with snapshot: %v", err)
	}
	if resp.Name != "dev-snap" {
		t.Errorf("unexpected name: %q", resp.Name)
	}
}

func TestClient_AddWorktree(t *testing.T) {
	script := makeScript(t, map[string]any{
		"add-worktree": AddWorktreeResponse{
			Status:  "ok",
			Repo:    "myrepo",
			Branch:  "feature-x",
			Path:    "/tmp/wt",
			EnvFile: ".env",
		},
	})

	c := NewClient(script, "env")
	resp, err := c.AddWorktree(context.Background(), "dev-1", "myrepo", "feature-x", "")
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if resp.Repo != "myrepo" || resp.Branch != "feature-x" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClient_ErrorResponse(t *testing.T) {
	script := makeScript(t, map[string]any{
		"fail:create": ErrorResponse{
			Status: "error",
			Error:  "environment already exists",
			Code:   "ENV_EXISTS",
		},
	})

	c := NewClient(script, "env")
	_, err := c.CreateEnv(context.Background(), "dev-1", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "environment already exists") {
		t.Errorf("error should contain message: %v", err)
	}
	if !strings.Contains(err.Error(), "ENV_EXISTS") {
		t.Errorf("error should contain code: %v", err)
	}
}

func TestClient_ListEnvs(t *testing.T) {
	script := makeScript(t, map[string]any{
		"list": ListEnvsResponse{
			Status: "ok",
			Environments: []EnvSummary{
				{Name: "dev-1", Index: 1, WorktreeCount: 2, CreatedAt: "2026-03-13T00:00:00Z"},
			},
		},
	})

	c := NewClient(script, "env")
	resp, err := c.ListEnvs(context.Background())
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	if len(resp.Environments) != 1 || resp.Environments[0].Name != "dev-1" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClient_DestroyEnv(t *testing.T) {
	script := makeScript(t, map[string]any{
		"destroy": map[string]string{"status": "ok"},
	})

	c := NewClient(script, "env")
	if err := c.DestroyEnv(context.Background(), "dev-1"); err != nil {
		t.Fatalf("DestroyEnv: %v", err)
	}
}
