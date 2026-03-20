package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/donovan-yohan/belayer/internal/v2/role"
)

// ParseRoute parses a Route from YAML bytes.
func ParseRoute(data []byte) (*Route, error) {
	var route Route
	if err := yaml.Unmarshal(data, &route); err != nil {
		return nil, fmt.Errorf("pipeline parse: %w", err)
	}
	if route.Safety == (role.SafetyConfig{}) {
		route.Safety = role.DefaultSafetyConfig()
	}
	return &route, nil
}

// ParseRouteFile reads and parses a Route from a YAML file, resolving `extends:`.
func ParseRouteFile(path string) (*Route, error) {
	return parseRouteFileWithVisited(path, nil)
}

func parseRouteFileWithVisited(path string, visited map[string]bool) (*Route, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[absPath] {
		return nil, fmt.Errorf("circular extends: %s", absPath)
	}
	visited[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read pipeline file: %w", err)
	}

	route, err := ParseRoute(data)
	if err != nil {
		return nil, err
	}

	// Resolve extends if present.
	if route.Extends != "" {
		if strings.Contains(route.Extends, "..") {
			return nil, fmt.Errorf("extends path must not contain '..': %s", route.Extends)
		}

		parentPath := filepath.Join(filepath.Dir(absPath), route.Extends)
		parent, err := parseRouteFileWithVisited(parentPath, visited)
		if err != nil {
			return nil, fmt.Errorf("resolve extends %q: %w", route.Extends, err)
		}

		route = mergeRoutes(parent, route)
	}

	return route, nil
}

// mergeRoutes applies child overrides on top of parent.
// Child's name, repos, and safety fully replace parent's.
// Phases are inherited if child declares none.
func mergeRoutes(parent, child *Route) *Route {
	merged := &Route{
		Name:   child.Name,
		Repos:  child.Repos,
		Safety: child.Safety,
	}

	// If child has no name, inherit parent's.
	if merged.Name == "" {
		merged.Name = parent.Name
	}

	// If child has no repos, inherit parent's.
	if len(merged.Repos) == 0 {
		merged.Repos = parent.Repos
	}

	// If child has no phases, inherit parent's.
	if len(child.Phases) == 0 {
		merged.Phases = parent.Phases
	} else {
		merged.Phases = child.Phases
	}

	// Safety: use child's if non-default, else parent's.
	if child.Safety == (role.SafetyConfig{}) || child.Safety == role.DefaultSafetyConfig() {
		merged.Safety = parent.Safety
	}

	return merged
}
