package vendor

import "strings"

// Interpolate replaces %{VAR} patterns in a prompt with values from the vars map.
// Unrecognized %{...} patterns are left untouched.
// Shell variables ($VAR) and agent skill invocations ($review, /ship) pass through literally.
func Interpolate(prompt string, vars map[string]string) string {
	for k, v := range vars {
		prompt = strings.ReplaceAll(prompt, "%{"+k+"}", v)
	}
	return prompt
}
