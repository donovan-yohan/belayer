package agent

import (
	"fmt"
	"sort"
	"sync"
)

// ToolRegistry holds named tools and provides thread-safe access.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry keyed by tool.Name.
// Returns an error if a tool with the same name is already registered.
func (r *ToolRegistry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[tool.Name]; exists {
		return fmt.Errorf("agent: tool %q already registered", tool.Name)
	}
	r.tools[tool.Name] = tool
	return nil
}

// Get returns the tool registered under name, or an error if not found.
func (r *ToolRegistry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return Tool{}, fmt.Errorf("agent: tool %q not registered", name)
	}
	return t, nil
}

// List returns all registered tools sorted by name.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools
}
