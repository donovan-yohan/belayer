package vendor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Interpolate replaces %{VAR} patterns in a prompt with values from the vars map.
// Unrecognized %{...} patterns are left untouched. This function is pure (no I/O).
func Interpolate(prompt string, vars map[string]string) string {
	for k, v := range vars {
		prompt = strings.ReplaceAll(prompt, "%{"+k+"}", v)
	}
	return prompt
}

// promptRefPattern matches $name references in prompts (e.g., $review).
var promptRefPattern = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)

// ResolvePromptRefs replaces $name references in a prompt with the contents of
// .belayer/prompts/<name>.md in the given workDir. Unresolvable references are
// left as-is and the last error is returned.
func ResolvePromptRefs(prompt string, workDir string) (string, error) {
	if !strings.Contains(prompt, "$") {
		return prompt, nil
	}
	var lastErr error
	resolved := promptRefPattern.ReplaceAllStringFunc(prompt, func(match string) string {
		name := match[1:] // strip leading $
		path := filepath.Join(workDir, ".belayer", "prompts", name+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = fmt.Errorf("prompt ref %q: %w", match, err)
			return match // leave unresolved
		}
		return strings.TrimSpace(string(data))
	})
	return resolved, lastErr
}
