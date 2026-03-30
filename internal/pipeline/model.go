package pipeline

// NodeType discriminates between constructive nodes and adversarial gates.
type NodeType string

const (
	NodeTypeNode  NodeType = "node"
	NodeTypeGate  NodeType = "gate"
	NodeTypeAgent NodeType = "agent"
)

// DimensionConfig defines a scoring dimension for a gate node.
type DimensionConfig struct {
	Name        string  `yaml:"name" json:"name"`
	Description string  `yaml:"description" json:"description"`
	Weight      float64 `yaml:"weight" json:"weight"`
	Rubric      string  `yaml:"rubric,omitempty" json:"rubric,omitempty"`
}

// ThresholdConfig defines score-based routing for a gate node.
type ThresholdConfig struct {
	Pass  float64 `yaml:"pass" json:"pass"`
	Retry float64 `yaml:"retry" json:"retry"`
}

// IntakeConfig defines an intake source in the pipeline.
type IntakeConfig struct {
	Name   string            `yaml:"name" json:"name"`
	Type   string            `yaml:"type" json:"type"`
	Check  string            `yaml:"check,omitempty" json:"check,omitempty"`
	Config map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
}

// SafetyConfig holds pipeline-wide safety limits.
type SafetyConfig struct {
	MaxConcurrentRuns int `yaml:"max_concurrent_runs,omitempty" json:"max_concurrent_runs,omitempty"`
}

// PipelineConfig is the top-level pipeline definition.
type PipelineConfig struct {
	Name   string         `yaml:"name" json:"name"`
	Nodes  []NodeConfig   `yaml:"nodes" json:"nodes"`
	Intake []IntakeConfig `yaml:"intake,omitempty" json:"intake,omitempty"`
	Safety SafetyConfig   `yaml:"safety,omitempty" json:"safety,omitempty"`
}

// NodeConfig defines a single pipeline node.
type NodeConfig struct {
	Name        string            `yaml:"name" json:"name"`
	Type        NodeType          `yaml:"type,omitempty" json:"type,omitempty"`
	Command     string            `yaml:"command,omitempty" json:"command,omitempty"`
	Vendor      string            `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Prompt      string            `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Description string            `yaml:"description" json:"description"`
	Input       InputConfig       `yaml:"input" json:"input"`
	Output      OutputConfig      `yaml:"output" json:"output"`
	Dimensions  []DimensionConfig `yaml:"dimensions,omitempty" json:"dimensions,omitempty"`
	Thresholds  ThresholdConfig   `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
	OnPass      string            `yaml:"on_pass" json:"on_pass"`
	OnRetry     string            `yaml:"on_retry" json:"on_retry"`
	OnFail      string            `yaml:"on_fail" json:"on_fail"`
	MaxRetries  int               `yaml:"max_retries" json:"max_retries"`
}

// InputConfig specifies what a node receives.
type InputConfig struct {
	Type string `yaml:"type" json:"type"`
	Key  string `yaml:"key" json:"key"`
}

// OutputConfig specifies what a node produces.
// Type is one of: file | commit | gate_result | pr
type OutputConfig struct {
	Type          string `yaml:"type" json:"type"`
	Path          string `yaml:"path,omitempty" json:"path,omitempty"`
	Key           string `yaml:"key,omitempty" json:"key,omitempty"`
	RationalePath string `yaml:"rationale_path,omitempty" json:"rationale_path,omitempty"`
}

// IsGate returns true if this node is a gate type.
// Agent nodes with dimensions are also treated as gates for scoring purposes.
func (n *NodeConfig) IsGate() bool {
	return n.Type == NodeTypeGate || (n.Type == NodeTypeAgent && len(n.Dimensions) > 0)
}

// IsAgent returns true if this node uses a vendor agent.
func (n *NodeConfig) IsAgent() bool {
	return n.Type == NodeTypeAgent
}

// EffectiveType returns the node's type, defaulting to "node".
func (n *NodeConfig) EffectiveType() NodeType {
	if n.Type == "" {
		return NodeTypeNode
	}
	return n.Type
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
