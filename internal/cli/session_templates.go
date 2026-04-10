package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/docker"
	"github.com/donovan-yohan/belayer/internal/session"
	"gopkg.in/yaml.v3"
)

type agentTemplateManifest struct {
	Description string `yaml:"description"`
	Vendor      string `yaml:"vendor"`
	Model       string `yaml:"model"`
	Ephemeral   bool   `yaml:"ephemeral"`
	Workspace   string `yaml:"workspace"`
	Tier        string `yaml:"tier,omitempty"`
}

func loadLaunchTemplate(templatesDir, belayerDir string, envCfg *docker.EnvironmentConfig, template, repo string) (session.SessionTemplate, error) {
	switch template {
	case "climb":
		if envCfg == nil {
			template = "implement"
			break
		}
		return buildEnvironmentSessionTemplate(belayerDir, envCfg, template, repo)
	case "climb-fullstack":
		if envCfg == nil {
			return session.SessionTemplate{}, fmt.Errorf("--environment is required for template %q", template)
		}
		return buildEnvironmentSessionTemplate(belayerDir, envCfg, template, repo)
	case "epic":
		return buildEpicSessionTemplate(belayerDir, envCfg)
	}
	return session.LoadTemplateFromDir(templatesDir, template)
}

func buildEnvironmentSessionTemplate(belayerDir string, envCfg *docker.EnvironmentConfig, template, repo string) (session.SessionTemplate, error) {
	if envCfg == nil {
		return session.SessionTemplate{}, fmt.Errorf("environment config is required")
	}

	agents := make([]session.AgentSpec, 0, len(envCfg.Agents)+1)
	added := map[string]bool{}

	addTemplate := func(templateName, repoName string) error {
		spec, err := loadAgentTemplateSpec(belayerDir, templateName, repoName)
		if err != nil {
			return err
		}
		if !added[spec.Name] {
			agents = append(agents, spec)
			added[spec.Name] = true
		}
		return nil
	}

	if err := addTemplate("pilot", ""); err != nil {
		return session.SessionTemplate{}, err
	}

	switch template {
	case "climb":
		selectedRepo := strings.TrimSpace(repo)
		for _, envAgent := range envCfg.Agents {
			if envAgent.Repo == "" {
				continue
			}
			if selectedRepo != "" && envAgent.Repo != selectedRepo {
				continue
			}
			if err := addTemplate(envAgent.Template, envAgent.Repo); err != nil {
				return session.SessionTemplate{}, err
			}
			selectedRepo = envAgent.Repo
			break
		}
		if selectedRepo == "" {
			return session.SessionTemplate{}, fmt.Errorf("no environment agent matched repo %q", repo)
		}
	case "climb-fullstack":
		for _, envAgent := range envCfg.Agents {
			if envAgent.Template == "pilot" {
				continue
			}
			if err := addTemplate(envAgent.Template, envAgent.Repo); err != nil {
				return session.SessionTemplate{}, err
			}
		}
	default:
		return session.SessionTemplate{}, fmt.Errorf("unsupported environment template %q", template)
	}

	if err := addTemplate("reviewer", ""); err != nil {
		return session.SessionTemplate{}, err
	}

	return session.SessionTemplate{
		Name:        template,
		Phase:       session.PhaseImplement,
		Description: fmt.Sprintf("%s session built from environment %s", template, envCfg.Name),
		Agents:      agents,
	}, nil
}

func buildEpicSessionTemplate(belayerDir string, envCfg *docker.EnvironmentConfig) (session.SessionTemplate, error) {
	pilot, err := loadAgentTemplateSpec(belayerDir, "pilot", "")
	if err != nil {
		return session.SessionTemplate{}, err
	}
	desc := "Epic orchestration session"
	if envCfg != nil && envCfg.Name != "" {
		desc = fmt.Sprintf("Epic orchestration session for environment %s", envCfg.Name)
	}
	return session.SessionTemplate{
		Name:        "epic",
		Phase:       session.PhaseImplement,
		Description: desc,
		Agents:      []session.AgentSpec{pilot},
	}, nil
}

func loadAgentTemplateSpec(belayerDir, templateName, repoName string) (session.AgentSpec, error) {
	dir := filepath.Join(belayerDir, "templates", templateName)
	manifestPath := filepath.Join(dir, "agent.yaml")
	promptPath := filepath.Join(dir, "system-prompt.md")

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return session.AgentSpec{}, fmt.Errorf("read agent template manifest %q: %w", manifestPath, err)
	}
	var manifest agentTemplateManifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return session.AgentSpec{}, fmt.Errorf("parse agent template manifest %q: %w", manifestPath, err)
	}

	systemPrompt, err := os.ReadFile(promptPath)
	if err != nil {
		return session.AgentSpec{}, fmt.Errorf("read system prompt %q: %w", promptPath, err)
	}

	tier := manifest.Tier
	if tier == "" {
		if manifest.Ephemeral {
			tier = "ephemeral"
		} else {
			tier = "main"
		}
	}

	if repoName == "" && manifest.Workspace != "" && manifest.Workspace != "none" && manifest.Workspace != "inherit" {
		repoName = manifest.Workspace
	}

	return session.AgentSpec{
		Name:         templateName,
		Vendor:       manifest.Vendor,
		Model:        manifest.Model,
		Repo:         repoName,
		Tier:         tier,
		Role:         manifest.Description,
		SystemPrompt: string(systemPrompt),
	}, nil
}
