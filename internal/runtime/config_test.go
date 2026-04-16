package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	belayerDir := filepath.Join(dir, ".belayer")
	if err := os.MkdirAll(belayerDir, 0o755); err != nil {
		t.Fatalf("mkdir .belayer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(belayerDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, dir string) // nil = no file
		wantCfg     Config
		wantErr     bool
		wantErrHas  string // substring the error must contain when wantErr is true
	}{
		{
			name: "happy path: full runtime section",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, `
runtime:
  up: "pnpm install --frozen-lockfile && pnpm dev &"
  health: "curl -sf http://localhost:4000/health"
  down: "pkill -f 'next dev'; pkill -f 'tsx watch'"
  endpoints:
    - {name: server, host: localhost, port: 4000}
    - {name: web, host: localhost, port: 3000}
`)
			},
			wantCfg: Config{
				Up:     "pnpm install --frozen-lockfile && pnpm dev &",
				Health: "curl -sf http://localhost:4000/health",
				Down:   "pkill -f 'next dev'; pkill -f 'tsx watch'",
				Endpoints: []Endpoint{
					{Name: "server", Host: "localhost", Port: 4000},
					{Name: "web", Host: "localhost", Port: 3000},
				},
			},
		},
		{
			name:    "missing file: returns zero config, nil error",
			setup:   nil,
			wantCfg: Config{},
		},
		{
			name: "file present, no runtime key: returns zero config, nil error",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, `
sandbox:
  image: ubuntu:24.04
worktrees:
  base: /tmp/worktrees
`)
			},
			wantCfg: Config{},
		},
		{
			name: "malformed YAML: returns non-nil error",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, `
runtime:
  up: [unclosed bracket
`)
			},
			wantErr:    true,
			wantErrHas: "runtime: parse config",
		},
		{
			name: "unreadable file: returns non-nil error",
			setup: func(t *testing.T, dir string) {
				if os.Getuid() == 0 {
					t.Skip("chmod 000 does not block root")
				}
				writeConfig(t, dir, `runtime:`)
				p := filepath.Join(dir, ".belayer", "config.yaml")
				if err := os.Chmod(p, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
			},
			wantErr:    true,
			wantErrHas: "runtime: read config",
		},
		{
			name: "partial config: only up set",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, `
runtime:
  up: "make dev"
`)
			},
			wantCfg: Config{
				Up: "make dev",
			},
		},
		{
			name: "endpoints in block style",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, `
runtime:
  up: "make dev"
  endpoints:
    - name: api
      host: 127.0.0.1
      port: 8080
    - name: ui
      host: 127.0.0.1
      port: 5173
`)
			},
			wantCfg: Config{
				Up: "make dev",
				Endpoints: []Endpoint{
					{Name: "api", Host: "127.0.0.1", Port: 8080},
					{Name: "ui", Host: "127.0.0.1", Port: 5173},
				},
			},
		},
		{
			name: "endpoints in flow style",
			setup: func(t *testing.T, dir string) {
				writeConfig(t, dir, `
runtime:
  endpoints:
    - {name: db, host: localhost, port: 5432}
`)
			},
			wantCfg: Config{
				Endpoints: []Endpoint{
					{Name: "db", Host: "localhost", Port: 5432},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, dir)
			}

			got, err := LoadConfig(dir)

			if tc.wantErr {
				if err == nil {
					t.Fatal("LoadConfig() returned nil error, want non-nil")
				}
				if tc.wantErrHas != "" && !strings.Contains(err.Error(), tc.wantErrHas) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tc.wantErrHas)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}

			if got.Up != tc.wantCfg.Up {
				t.Errorf("Up = %q, want %q", got.Up, tc.wantCfg.Up)
			}
			if got.Health != tc.wantCfg.Health {
				t.Errorf("Health = %q, want %q", got.Health, tc.wantCfg.Health)
			}
			if got.Down != tc.wantCfg.Down {
				t.Errorf("Down = %q, want %q", got.Down, tc.wantCfg.Down)
			}
			if len(got.Endpoints) != len(tc.wantCfg.Endpoints) {
				t.Fatalf("len(Endpoints) = %d, want %d", len(got.Endpoints), len(tc.wantCfg.Endpoints))
			}
			for i, e := range got.Endpoints {
				want := tc.wantCfg.Endpoints[i]
				if e != want {
					t.Errorf("Endpoints[%d] = %+v, want %+v", i, e, want)
				}
			}
		})
	}
}
