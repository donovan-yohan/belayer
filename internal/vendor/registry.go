package vendor

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds named vendor adapters and provides thread-safe access.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

// Register adds an adapter to the registry keyed by adapter.Name().
// Overwrites any previously registered adapter with the same name.
func (r *Registry) Register(adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.Name()] = adapter
}

// Get returns the adapter registered under name, or an error if not found.
func (r *Registry) Get(name string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("vendor: adapter %q not registered", name)
	}
	return a, nil
}

// List returns a sorted slice of all registered adapter names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DefaultRegistry returns a Registry pre-populated with Claude, Codex, and
// a Generic "opencode" adapter.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(ClaudeAdapter{})
	r.Register(CodexAdapter{})
	r.Register(NewGenericAdapter("opencode", "opencode"))
	return r
}
