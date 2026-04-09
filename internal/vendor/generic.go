package vendor

import "fmt"

// GenericAdapter implements Adapter for any CLI agent with unstructured output.
// It performs no JSON parsing — every line is returned as a raw text event.
type GenericAdapter struct {
	name string
	cmd  string
}

// NewGenericAdapter creates a GenericAdapter with the given name and launch command.
func NewGenericAdapter(name, cmd string) GenericAdapter {
	return GenericAdapter{name: name, cmd: cmd}
}

// Name returns the configured adapter name.
func (a GenericAdapter) Name() string { return a.name }

// LaunchCmd returns the configured command string. workDir and systemPrompt are ignored.
func (a GenericAdapter) LaunchCmd(workDir string, systemPrompt string) string {
	return a.cmd
}

// ParseOutput returns every line as a raw text OutputEvent. No token tracking.
func (a GenericAdapter) ParseOutput(line string) (OutputEvent, error) {
	return OutputEvent{Type: "text", Content: line, Raw: line}, nil
}

// CompileRestartPrompt wraps context in a generic continuation prompt.
func (a GenericAdapter) CompileRestartPrompt(context string) string {
	return fmt.Sprintf("Continue from where you left off. Previous context:\n\n%s", context)
}
