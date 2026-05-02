package generatedtalent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const SchemaVersion = "belayer-generated-talent/v1"

type Record struct {
	SchemaVersion     string            `json:"schema_version" yaml:"schema_version"`
	ID                string            `json:"id" yaml:"id"`
	Domain            string            `json:"domain" yaml:"domain"`
	Role              string            `json:"role" yaml:"role"`
	Lifecycle         string            `json:"lifecycle" yaml:"lifecycle"`
	Status            string            `json:"status" yaml:"status"`
	SourceRequest     string            `json:"source_request" yaml:"source_request"`
	Reason            string            `json:"reason" yaml:"reason"`
	Metadata          map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	PromotionEvidence []string          `json:"promotion_evidence,omitempty" yaml:"promotion_evidence,omitempty"`
	CreatedAt         string            `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt         string            `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

type InputError struct {
	Err error
}

func (e InputError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e InputError) Unwrap() error {
	return e.Err
}

func inputErrorf(format string, args ...any) error {
	return InputError{Err: fmt.Errorf(format, args...)}
}

func IsInputError(err error) bool {
	var inputErr InputError
	return errors.As(err, &inputErr)
}

func Normalize(record Record) Record {
	if record.SchemaVersion == "" {
		record.SchemaVersion = SchemaVersion
	}
	if record.Lifecycle == "" {
		record.Lifecycle = "ephemeral"
	}
	if record.Status == "" {
		record.Status = "generated"
	}
	return record
}

func ValidateRecord(record Record) error {
	record = Normalize(record)
	required := []struct {
		label string
		value string
	}{
		{label: "id", value: record.ID},
		{label: "domain", value: record.Domain},
		{label: "role", value: record.Role},
		{label: "source_request", value: record.SourceRequest},
		{label: "reason", value: record.Reason},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return inputErrorf("%s is required", field.label)
		}
	}
	if err := validateIdentifier(record.ID, "id"); err != nil {
		return err
	}
	switch record.Lifecycle {
	case "resident", "resumable", "ephemeral":
	default:
		return inputErrorf("invalid lifecycle %q: must be resident, resumable, or ephemeral", record.Lifecycle)
	}
	switch record.Status {
	case "generated", "promoted", "retired", "discarded":
	default:
		return inputErrorf("invalid generated talent status %q: must be generated, promoted, retired, or discarded", record.Status)
	}
	for _, evidence := range record.PromotionEvidence {
		if strings.TrimSpace(evidence) == "" {
			return inputErrorf("promotion_evidence values must be non-empty")
		}
	}
	if record.Status == "promoted" && len(record.PromotionEvidence) == 0 {
		return inputErrorf("promotion_evidence is required when status is promoted")
	}
	return nil
}

func validateIdentifier(value, label string) error {
	switch value {
	case ".", "..":
		return inputErrorf("%s %q is reserved", label, value)
	}
	if strings.ContainsAny(value, `/\`) {
		return inputErrorf("%s %q must not contain path separators", label, value)
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return inputErrorf("%s %q must contain only letters, numbers, dash, underscore, or dot", label, value)
	}
	return nil
}

func WriteRecord(path string, record Record) error {
	record = Normalize(record)
	if err := ValidateRecord(record); err != nil {
		return err
	}
	raw, err := yaml.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal generated talent: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir generated talent: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write generated talent: %w", err)
	}
	return nil
}

func ReadRecord(path string) (Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := yaml.Unmarshal(raw, &record); err != nil {
		return Record{}, fmt.Errorf("parse generated talent: %w", err)
	}
	record = Normalize(record)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = filepath.Base(filepath.Dir(path))
	}
	if err := ValidateRecord(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func ScaffoldIdentity(projectRoot string, record Record, force bool) (string, error) {
	record = Normalize(record)
	if err := ValidateRecord(record); err != nil {
		return "", err
	}
	identityDir := filepath.Join(projectRoot, ".belayer", "agents", record.ID)
	if !force {
		if _, err := os.Stat(identityDir); err == nil {
			return "", inputErrorf("generated talent identity %q already exists (use --force to overwrite)", record.ID)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat generated talent identity: %w", err)
		}
	}
	if err := os.MkdirAll(identityDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir generated talent identity: %w", err)
	}
	agentConfig, err := agentYAML(record)
	if err != nil {
		return "", err
	}
	files := map[string]string{
		"agent.yaml":       agentConfig,
		"system-prompt.md": systemPrompt(record),
		"agents.md":        agentsMD(record),
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(identityDir, rel), []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", rel, err)
		}
	}
	if err := WriteRecord(filepath.Join(identityDir, "talent.yaml"), record); err != nil {
		return "", err
	}
	return identityDir, nil
}

func agentYAML(record Record) (string, error) {
	kind := "side"
	maxTurns := 40
	if record.Lifecycle == "resident" {
		kind = "main"
		maxTurns = 80
	}
	ephemeral := record.Lifecycle == "ephemeral"
	raw, err := yaml.Marshal(struct {
		SchemaVersion string   `yaml:"schema_version"`
		Description   string   `yaml:"description"`
		Kind          string   `yaml:"kind"`
		Vendor        string   `yaml:"vendor"`
		Model         string   `yaml:"model"`
		MaxTurns      int      `yaml:"max_turns"`
		MaxDuration   string   `yaml:"max_duration"`
		Ephemeral     bool     `yaml:"ephemeral"`
		Workspace     string   `yaml:"workspace"`
		BelayerTools  []string `yaml:"belayer_tools"`
	}{
		SchemaVersion: "1",
		Description:   fmt.Sprintf("Generated talent %s (%s/%s)", record.ID, record.Domain, record.Role),
		Kind:          kind,
		Vendor:        "codex",
		Model:         "gpt-5.4",
		MaxTurns:      maxTurns,
		MaxDuration:   "45m",
		Ephemeral:     ephemeral,
		Workspace:     "inherit",
		BelayerTools:  []string{},
	})
	if err != nil {
		return "", fmt.Errorf("marshal generated talent agent config: %w", err)
	}
	return string(raw), nil
}

func systemPrompt(record Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are generated talent `%s`.\n\n", record.ID)
	b.WriteString("Mechanical contract:\n")
	fmt.Fprintf(&b, "- domain: %s\n", record.Domain)
	fmt.Fprintf(&b, "- role: %s\n", record.Role)
	fmt.Fprintf(&b, "- lifecycle: %s\n", record.Lifecycle)
	fmt.Fprintf(&b, "- source request: %s\n", record.SourceRequest)
	fmt.Fprintf(&b, "- reason: %s\n", record.Reason)
	if len(record.Metadata) > 0 {
		b.WriteString("\nCaller-provided metadata:\n")
		keys := make([]string, 0, len(record.Metadata))
		for key := range record.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, "- %s: %s\n", key, record.Metadata[key])
		}
	}
	b.WriteString("\nStay inside the assignment context you receive. Produce bounded output for the coordinator and do not silently mutate global crag or catalog state.\n")
	return b.String()
}

func agentsMD(record Record) string {
	return fmt.Sprintf(
		"# Generated Talent: %s\n\n"+
			"This identity was scaffolded from a generated talent record.\n\n"+
			"- Domain: `%s`\n"+
			"- Role: `%s`\n"+
			"- Lifecycle: `%s`\n"+
			"- Source request: `%s`\n\n"+
			"Use normal Belayer mail and artifact tools granted to this identity. Promotion\n"+
			"into a durable catalog talent is a separate reviewed step.\n",
		record.ID, record.Domain, record.Role, record.Lifecycle, record.SourceRequest,
	)
}
