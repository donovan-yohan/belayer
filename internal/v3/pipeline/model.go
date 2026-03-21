package pipeline

// PipelineConfig is the top-level pipeline definition.
type PipelineConfig struct {
	Name  string       `yaml:"name" json:"name"`
	Nodes []NodeConfig `yaml:"nodes" json:"nodes"`
}

// NodeConfig defines a single pipeline node.
type NodeConfig struct {
	Name        string       `yaml:"name" json:"name"`
	Description string       `yaml:"description" json:"description"`
	Input       InputConfig  `yaml:"input" json:"input"`
	Output      OutputConfig `yaml:"output" json:"output"`
	OnPass      string       `yaml:"on_pass" json:"on_pass"`
	OnRetry     string       `yaml:"on_retry" json:"on_retry"`
	OnFail      string       `yaml:"on_fail" json:"on_fail"`
	MaxRetries  int          `yaml:"max_retries" json:"max_retries"`
}

// InputConfig specifies what a node receives.
type InputConfig struct {
	Type string `yaml:"type" json:"type"`
	Key  string `yaml:"key" json:"key"`
}

// OutputConfig specifies what a node produces.
type OutputConfig struct {
	Type string `yaml:"type" json:"type"`
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	Key  string `yaml:"key,omitempty" json:"key,omitempty"`
}

// OutputKey returns the artifact key for this node's output.
func (n *NodeConfig) OutputKey() string {
	if n.Output.Key != "" {
		return n.Output.Key
	}
	return n.Name
}

// FindNode returns the node with the given name, or nil.
func (p *PipelineConfig) FindNode(name string) *NodeConfig {
	for i := range p.Nodes {
		if p.Nodes[i].Name == name {
			return &p.Nodes[i]
		}
	}
	return nil
}

// NodeNames returns all node names in order.
func (p *PipelineConfig) NodeNames() []string {
	names := make([]string, len(p.Nodes))
	for i, n := range p.Nodes {
		names[i] = n.Name
	}
	return names
}
