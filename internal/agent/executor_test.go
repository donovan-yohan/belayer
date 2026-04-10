package agent

import (
	"context"
	"testing"
)

// TestRenderCommand verifies that command templates are rendered with
// shell-quoted values and that injection attempts are safely neutralised.
func TestRenderCommand(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		input   map[string]string
		want    string
		wantErr bool
	}{
		{
			name:  "simple substitution",
			tmpl:  "echo {{.msg}}",
			input: map[string]string{"msg": "hello"},
			want:  "echo 'hello'",
		},
		{
			name:  "multiple fields",
			tmpl:  "psql $DB -c {{.query}} -U {{.user}}",
			input: map[string]string{"query": "SELECT 1", "user": "admin"},
			want:  "psql $DB -c 'SELECT 1' -U 'admin'",
		},
		{
			name:  "injection attempt with single quotes",
			tmpl:  "echo {{.msg}}",
			input: map[string]string{"msg": "'; rm -rf /; echo '"},
			want:  "echo ''\"'\"'; rm -rf /; echo '\"'\"''",
		},
		{
			name:  "subshell injection attempt",
			tmpl:  "greet {{.name}}",
			input: map[string]string{"name": "$(cat /etc/passwd)"},
			want:  "greet '$(cat /etc/passwd)'",
		},
		{
			name:  "backtick injection",
			tmpl:  "greet {{.name}}",
			input: map[string]string{"name": "`id`"},
			want:  "greet '`id`'",
		},
		{
			name:  "empty input value",
			tmpl:  "echo {{.val}}",
			input: map[string]string{"val": ""},
			want:  "echo ''",
		},
		{
			name:    "missing key",
			tmpl:    "echo {{.missing}}",
			input:   map[string]string{},
			wantErr: true,
		},
		{
			name:    "empty template",
			tmpl:    "",
			input:   map[string]string{},
			wantErr: true,
		},
		{
			name:  "no substitutions",
			tmpl:  "ls -la",
			input: map[string]string{},
			want:  "ls -la",
		},
		{
			name:  "sql query with injection",
			tmpl:  "psql $DB -c {{.query}}",
			input: map[string]string{"query": "SELECT * FROM users WHERE name = 'admin'"},
			want:  "psql $DB -c 'SELECT * FROM users WHERE name = '\"'\"'admin'\"'\"''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderCommand(tt.tmpl, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("renderCommand(%q, %v) expected error, got nil (result: %q)", tt.tmpl, tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("renderCommand(%q, %v) unexpected error: %v", tt.tmpl, tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("renderCommand(%q, %v)\n  got  %q\n  want %q", tt.tmpl, tt.input, got, tt.want)
			}
		})
	}
}

// TestExecutorValidation checks that Execute rejects invalid tool specs
// before attempting execution.
func TestExecutorValidation(t *testing.T) {
	ex := &Executor{SandboxDir: "/nonexistent"}

	tests := []struct {
		name    string
		spec    ToolSpec
		wantErr bool
	}{
		{
			name: "empty target",
			spec: ToolSpec{
				Name: "test",
				Exec: ToolExec{Target: "", Command: "echo hi"},
			},
			wantErr: true,
		},
		{
			name: "invalid target",
			spec: ToolSpec{
				Name: "test",
				Exec: ToolExec{Target: "foobar", Command: "echo hi"},
			},
			wantErr: true,
		},
		{
			name: "missing sandbox dir for compose target",
			spec: ToolSpec{
				Name: "test",
				Exec: ToolExec{Target: "agent", Command: "echo hi"},
			},
			wantErr: true, // compose file won't be found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ex.Execute(context.Background(), tt.spec, map[string]string{})
			if tt.wantErr && err == nil {
				t.Errorf("Execute() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Execute() unexpected error: %v", err)
			}
		})
	}
}

// TestHostExecution verifies that the "host" target runs commands directly.
func TestHostExecution(t *testing.T) {
	ex := &Executor{}
	spec := ToolSpec{
		Name:        "echo-test",
		Description: "Echo a message",
		Exec: ToolExec{
			Target:  "host",
			Command: "echo {{.msg}}",
			Timeout: 5,
		},
	}

	result, err := ex.Execute(context.Background(), spec, map[string]string{"msg": "hello world"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	// echo adds a newline; the quoted value is 'hello world'
	want := "hello world\n"
	if result.Stdout != want {
		t.Errorf("Stdout = %q, want %q", result.Stdout, want)
	}
}

// TestHostExecutionExitCode checks that non-zero exit codes are returned correctly.
func TestHostExecutionExitCode(t *testing.T) {
	ex := &Executor{}
	spec := ToolSpec{
		Name: "fail",
		Exec: ToolExec{
			Target:  "host",
			Command: "exit 42",
			Timeout: 5,
		},
	}
	result, err := ex.Execute(context.Background(), spec, map[string]string{})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", result.ExitCode)
	}
}

// TestToolExecEffectiveTimeout ensures defaults are applied correctly.
func TestToolExecEffectiveTimeout(t *testing.T) {
	tests := []struct {
		timeout int
		want    int
	}{
		{0, 60},
		{-1, 60},
		{30, 30},
		{120, 120},
	}
	for _, tt := range tests {
		exec := ToolExec{Timeout: tt.timeout}
		if got := exec.EffectiveTimeout(); got != tt.want {
			t.Errorf("EffectiveTimeout(%d) = %d, want %d", tt.timeout, got, tt.want)
		}
	}
}
