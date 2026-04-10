package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewWorkbench_RequiresSessionID(t *testing.T) {
	_, err := NewWorkbench(WorkbenchConfig{
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "test"}},
		},
	})
	if err == nil {
		t.Fatal("expected error when SessionID is empty, got nil")
	}
	if !strings.Contains(err.Error(), "SessionID is required") {
		t.Errorf("expected error about SessionID, got: %v", err)
	}
}

func TestNewWorkbench_RejectsPathTraversalSessionID(t *testing.T) {
	for _, bad := range []string{"../evil", "a/b", "a\\b"} {
		_, err := NewWorkbench(WorkbenchConfig{
			SessionID: bad,
			Spec: WorkbenchConfigSpec{
				Services: []ServiceDecl{{Name: "test"}},
			},
		})
		if err == nil {
			t.Fatalf("expected error for SessionID %q, got nil", bad)
		}
		if !strings.Contains(err.Error(), "invalid path characters") {
			t.Errorf("expected path character error for %q, got: %v", bad, err)
		}
	}
}

func TestNewWorkbench_RequiresServices(t *testing.T) {
	_, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		Spec:      WorkbenchConfigSpec{},
	})
	if err == nil {
		t.Fatal("expected error when Services is empty, got nil")
	}
	if !strings.Contains(err.Error(), "Spec.Services is required") {
		t.Errorf("expected error about Services, got: %v", err)
	}
}

func TestNewWorkbench_AppliesDefaultTimeout(t *testing.T) {
	w, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "test"}},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkbench returned error: %v", err)
	}
	if w.config.Spec.Timeout != "5m" {
		t.Errorf("expected default timeout '5m', got %q", w.config.Spec.Timeout)
	}
}

func TestNewWorkbench_AppliesDefaultNetworks(t *testing.T) {
	w, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "test"}},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkbench returned error: %v", err)
	}
	if len(w.config.Networks) != 2 {
		t.Errorf("expected 2 default networks, got %d", len(w.config.Networks))
	}
	if w.config.Networks[0] != "workbench-net" {
		t.Errorf("expected first network 'workbench-net', got %q", w.config.Networks[0])
	}
	if w.config.Networks[1] != "infra-net" {
		t.Errorf("expected second network 'infra-net', got %q", w.config.Networks[1])
	}
}

func TestGenerateWorkbenchCompose_ServiceNames(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{Name: "api", Image: "extend/api:latest"},
				{Name: "db", Image: "postgres:15"},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	for _, name := range []string{"api:", "db:"} {
		if !strings.Contains(content, name) {
			t.Errorf("expected %q service in compose output, got:\n%s", name, content)
		}
	}
}

func TestGenerateWorkbenchCompose_BuildContext(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name:  "api",
					Build: BuildDecl{Context: "/path/to/api"},
				},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "build:") {
		t.Errorf("expected 'build:' in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "context: /path/to/api") {
		t.Errorf("expected build context in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_Image(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name:  "db",
					Image: "postgres:15",
				},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "image: postgres:15") {
		t.Errorf("expected 'image: postgres:15' in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_Environment(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name: "api",
					Env: map[string]string{
						"DATABASE_URL": "postgres://localhost:5432/db",
						"NODE_ENV":     "development",
					},
				},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "environment:") {
		t.Errorf("expected 'environment:' in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "DATABASE_URL:") {
		t.Errorf("expected DATABASE_URL in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "NODE_ENV:") {
		t.Errorf("expected NODE_ENV in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_EnvironmentQuoting(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name: "api",
					Env: map[string]string{
						"QUOTED": `value with "quotes"`,
					},
				},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	// Values should be safely quoted (Go %q escapes inner quotes)
	if !strings.Contains(content, `QUOTED: "value with \"quotes\""`) {
		t.Errorf("expected safely quoted env value in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_DependsOn(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name:      "api",
					Image:     "extend/api:latest",
					DependsOn: map[string]ServiceDependency{"db": {}},
				},
				{
					Name:  "db",
					Image: "postgres:15",
				},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "depends_on:") {
		t.Errorf("expected 'depends_on:' in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "db:") || !strings.Contains(content, "condition: service_started") {
		t.Errorf("expected structured db dependency in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_HealthCheck(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name:  "db",
					Image: "postgres:15",
					Health: &HealthDecl{
						Test:     []string{"CMD", "pg_isready", "-U", "postgres"},
						Interval: "5s",
						Timeout:  "3s",
						Retries:  5,
					},
				},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "healthcheck:") {
		t.Errorf("expected 'healthcheck:' in compose output, got:\n%s", content)
	}
	// Healthcheck test should be serialized as a valid JSON array
	if !strings.Contains(content, `test: ["CMD","pg_isready","-U","postgres"]`) {
		t.Errorf("expected healthcheck test as JSON array in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "interval: 5s") {
		t.Errorf("expected interval in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "timeout: 3s") {
		t.Errorf("expected timeout in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "retries: 5") {
		t.Errorf("expected retries in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_HealthCheckStartPeriod(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name:  "api",
					Image: "extend/api:latest",
					Health: &HealthDecl{
						Test:        []string{"CMD", "curl", "-f", "http://localhost:8080/health"},
						Interval:    "5s",
						Timeout:     "3s",
						Retries:     10,
						StartPeriod: "30s",
					},
				},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "start_period: 30s") {
		t.Errorf("expected start_period in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_Networks(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "api", Image: "extend/api:latest"}},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "workbench-net:") {
		t.Errorf("expected 'workbench-net:' in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "internal: true") {
		t.Errorf("expected 'internal: true' for workbench-net, got:\n%s", content)
	}
	if !strings.Contains(content, "infra-net:") {
		t.Errorf("expected 'infra-net:' in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "driver: bridge") {
		t.Errorf("expected 'driver: bridge' for infra-net, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_NetworkNames(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "sess-123",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "api", Image: "extend/api:latest"}},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "name: workbench-sess-123") {
		t.Errorf("expected network name 'workbench-sess-123' in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "name: infra-sess-123") {
		t.Errorf("expected network name 'infra-sess-123' in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_WorktreePathSubstitution(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name:  "api",
					Build: BuildDecl{Context: "${WORKTREE_extend_api}"},
				},
			},
		},
		WorktreePaths: map[string]string{
			"extend_api": "/Users/test/worktrees/extend-api",
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "/Users/test/worktrees/extend-api") {
		t.Errorf("expected substituted path in compose output, got:\n%s", content)
	}
	if strings.Contains(content, "${WORKTREE_extend_api}") {
		t.Errorf("expected placeholder to be substituted, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_WorktreeDotTemplateSubstitution(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name:  "api",
					Build: BuildDecl{Context: "{{.Worktree.extend-api}}"},
				},
			},
		},
		WorktreePaths: map[string]string{
			"extend-api": "/Users/test/worktrees/extend-api",
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "/Users/test/worktrees/extend-api") {
		t.Errorf("expected substituted path in compose output, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_ExplicitInfraNetwork(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{
					Name:     "db",
					Image:    "postgres:15",
					Networks: []string{"infra-net"},
				},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "- infra-net") {
		t.Errorf("expected infra-net in service networks, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_NoExtraNetworksByDefault(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{Name: "api", Image: "extend/api:latest"},
			},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if strings.Contains(content, "- infra-net") {
		t.Errorf("expected no infra-net for service without Networks, got:\n%s", content)
	}
}

func TestGenerateWorkbenchCompose_Version(t *testing.T) {
	cfg := WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "api", Image: "extend/api:latest"}},
		},
	}
	out, err := generateWorkbenchCompose(cfg)
	if err != nil {
		t.Fatalf("generateWorkbenchCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "version: '3.9'") {
		t.Errorf("expected version '3.9' in compose output, got:\n%s", content)
	}
}

func TestWorkbench_Create_SetsComposeDir(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		BaseDir:   dir,
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "api", Image: "extend/api:latest"}},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkbench returned error: %v", err)
	}

	if err := w.Create(); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if w.ComposeDir() == "" {
		t.Error("expected ComposeDir to be set after Create")
	}

	// Verify compose file was actually written
	composePath := filepath.Join(w.ComposeDir(), "docker-compose.yml")
	if _, err := os.Stat(composePath); err != nil {
		t.Errorf("expected compose file at %s, got error: %v", composePath, err)
	}
}

func TestWorkbench_Start_RequiresCreate(t *testing.T) {
	w, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "api", Image: "extend/api:latest"}},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkbench returned error: %v", err)
	}

	err = w.Start()
	if err == nil {
		t.Fatal("expected error when Start is called before Create, got nil")
	}
	if !strings.Contains(err.Error(), "must call Create before Start") {
		t.Errorf("expected error about Create, got: %v", err)
	}
}

func TestWorkbench_Status_RequiresCreate(t *testing.T) {
	w, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "api", Image: "extend/api:latest"}},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkbench returned error: %v", err)
	}

	_, err = w.Status()
	if err == nil {
		t.Fatal("expected error when Status is called before Create, got nil")
	}
	if !strings.Contains(err.Error(), "must call Create before Status") {
		t.Errorf("expected error about Create, got: %v", err)
	}
}

func TestWorkbench_WaitForHealthy_RequiresCreate(t *testing.T) {
	w, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "api", Image: "extend/api:latest"}},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkbench returned error: %v", err)
	}

	err = w.WaitForHealthy(1 * time.Second)
	if err == nil {
		t.Fatal("expected error when WaitForHealthy is called before Create, got nil")
	}
	if !strings.Contains(err.Error(), "must call Create before WaitForHealthy") {
		t.Errorf("expected error about Create, got: %v", err)
	}
}

func TestWorkbench_Stop_RequiresCreate(t *testing.T) {
	w, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{{Name: "api", Image: "extend/api:latest"}},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkbench returned error: %v", err)
	}

	err = w.Stop()
	if err == nil {
		t.Fatal("expected error when Stop is called before Create, got nil")
	}
	if !strings.Contains(err.Error(), "must call Create before Stop") {
		t.Errorf("expected error about Create, got: %v", err)
	}
}

func TestWorkbench_Endpoints(t *testing.T) {
	w, err := NewWorkbench(WorkbenchConfig{
		SessionID: "test-session",
		Spec: WorkbenchConfigSpec{
			Services: []ServiceDecl{
				{Name: "extend-api", Ports: []string{"8080"}},
				{Name: "postgres", Ports: []string{"5432"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkbench returned error: %v", err)
	}

	endpoints := w.Endpoints()
	if endpoints["extend-api"].URL != "http://extend-api:8080" {
		t.Fatalf("unexpected extend-api endpoint: %#v", endpoints["extend-api"])
	}
	if endpoints["postgres"].URL != "postgres:5432" {
		t.Fatalf("unexpected postgres endpoint: %#v", endpoints["postgres"])
	}
}
