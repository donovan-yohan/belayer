package pipeline

import "fmt"

// AvailableTemplates returns the names of all built-in pipeline templates.
func AvailableTemplates() []string {
	return []string{"solo", "team"}
}

// LoadTemplate reads a built-in pipeline template by name.
func LoadTemplate(name string) ([]byte, error) {
	path := fmt.Sprintf("templates/%s.yaml", name)
	data, err := templateFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unknown template %q (available: solo, team)", name)
	}
	return data, nil
}
