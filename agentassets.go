package belayerassets

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:generate go run ./cmd/gencodexskills

//go:embed plugins/harness/commands/*.md plugins/pr/commands/*.md plugins/explorer/commands/*.md plugins/harness/skills/strangler-fig all:plugins/harness/.claude-plugin/plugin.json all:plugins/pr/.claude-plugin/plugin.json all:plugins/explorer/.claude-plugin/plugin.json
var FS embed.FS

type pluginJSON struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
	Skills      []string `json:"skills"`
}

type PluginSpec struct {
	Name        string
	Version     string
	Description string
	Commands    []CommandSpec
	Skills      []StaticSkillSpec
}

type CommandSpec struct {
	Plugin      string
	Name        string
	Title       string
	Description string
	SourcePath  string
	Body        string
}

type StaticSkillSpec struct {
	Plugin    string
	Name      string
	SourceDir string
}

var (
	superpowersRef = regexp.MustCompile(`superpowers:([a-z-]+)`)

	loadOnce sync.Once
	loadErr  error
	specs    []PluginSpec
)

func Plugins() ([]PluginSpec, error) {
	loadOnce.Do(func() {
		specs, loadErr = loadPlugins()
	})
	if loadErr != nil {
		return nil, loadErr
	}

	out := make([]PluginSpec, len(specs))
	copy(out, specs)
	return out, nil
}

func PluginVersion(name string) (string, error) {
	plugins, err := Plugins()
	if err != nil {
		return "", err
	}
	for _, plugin := range plugins {
		if plugin.Name == name {
			return plugin.Version, nil
		}
	}
	return "", fmt.Errorf("unknown plugin %q", name)
}

func MustPluginVersion(name string) string {
	version, err := PluginVersion(name)
	if err != nil {
		panic(err)
	}
	return version
}

func CodexPackVersion() string {
	plugins, err := Plugins()
	if err != nil {
		panic(err)
	}
	parts := make([]string, len(plugins))
	for i, p := range plugins {
		parts[i] = p.Name + "-" + p.Version
	}
	return strings.Join(parts, "_")
}

func Invocation(provider, plugin, name string) string {
	if strings.EqualFold(provider, "codex") {
		return plugin + "-" + name
	}
	return "/" + plugin + ":" + name
}

func CodexSkillFiles() (map[string][]byte, error) {
	plugins, err := Plugins()
	if err != nil {
		return nil, err
	}

	files := make(map[string][]byte)
	for _, plugin := range plugins {
		for _, command := range plugin.Commands {
			target := path.Join(command.CodexSkillName(), "SKILL.md")
			files[target] = []byte(command.RenderCodexSkill())
		}
		for _, skill := range plugin.Skills {
			if err := appendStaticSkill(files, skill); err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}

func (c CommandSpec) ClaudeCommand() string {
	return Invocation("claude", c.Plugin, c.Name)
}

func (c CommandSpec) CodexSkillName() string {
	return Invocation("codex", c.Plugin, c.Name)
}

func (c CommandSpec) RenderCodexSkill() string {
	body := rewriteForCodex(c.Body, "")
	return fmt.Sprintf(`---
name: %s
description: %s
---

> Generated from Claude plugin command: %s
> Claude alias: %s

%s
`, c.CodexSkillName(), c.Description, c.SourcePath, c.ClaudeCommand(), strings.TrimSpace(body))
}

func loadPlugins() ([]PluginSpec, error) {
	pluginNames := []string{"harness", "pr", "explorer"}
	loaded := make([]PluginSpec, 0, len(pluginNames))
	for _, pluginName := range pluginNames {
		spec, err := loadPlugin(pluginName)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, spec)
	}
	return loaded, nil
}

func loadPlugin(pluginName string) (PluginSpec, error) {
	pluginJSONPath := path.Join("plugins", pluginName, ".claude-plugin", "plugin.json")
	raw, err := FS.ReadFile(pluginJSONPath)
	if err != nil {
		return PluginSpec{}, fmt.Errorf("read %s: %w", pluginJSONPath, err)
	}

	var decoded pluginJSON
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return PluginSpec{}, fmt.Errorf("parse %s: %w", pluginJSONPath, err)
	}

	spec := PluginSpec{
		Name:        decoded.Name,
		Version:     decoded.Version,
		Description: decoded.Description,
	}

	for _, relPath := range decoded.Commands {
		sourcePath := path.Join("plugins", decoded.Name, strings.TrimPrefix(relPath, "./"))
		command, err := loadCommand(decoded.Name, sourcePath)
		if err != nil {
			return PluginSpec{}, err
		}
		spec.Commands = append(spec.Commands, command)
	}

	for _, relPath := range decoded.Skills {
		sourceDir := path.Join("plugins", decoded.Name, strings.TrimPrefix(relPath, "./"))
		spec.Skills = append(spec.Skills, StaticSkillSpec{
			Plugin:    decoded.Name,
			Name:      path.Base(sourceDir),
			SourceDir: sourceDir,
		})
	}

	return spec, nil
}

func loadCommand(pluginName, sourcePath string) (CommandSpec, error) {
	raw, err := FS.ReadFile(sourcePath)
	if err != nil {
		return CommandSpec{}, fmt.Errorf("read %s: %w", sourcePath, err)
	}

	description, body, err := splitFrontmatter(string(raw))
	if err != nil {
		return CommandSpec{}, fmt.Errorf("parse %s: %w", sourcePath, err)
	}

	return CommandSpec{
		Plugin:      pluginName,
		Name:        strings.TrimSuffix(path.Base(sourcePath), path.Ext(sourcePath)),
		Title:       extractTitle(body),
		Description: description,
		SourcePath:  sourcePath,
		Body:        body,
	}, nil
}

func splitFrontmatter(content string) (description, body string, err error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content, nil
	}

	rest := strings.TrimPrefix(content, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		return "", "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	frontmatter := rest[:idx]
	body = rest[idx+len("\n---\n"):]

	for _, line := range strings.Split(frontmatter, "\n") {
		if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			break
		}
	}

	return description, body, nil
}

func extractTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func appendStaticSkill(files map[string][]byte, skill StaticSkillSpec) error {
	var paths []string
	if err := fs.WalkDir(FS, skill.SourceDir, func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, current)
		return nil
	}); err != nil {
		return fmt.Errorf("walk %s: %w", skill.SourceDir, err)
	}

	sort.Strings(paths)
	prefix := skill.SourceDir + "/"
	for _, sourcePath := range paths {
		relPath := strings.TrimPrefix(sourcePath, prefix)
		target := path.Join(skill.Name, relPath)
		raw, err := FS.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", sourcePath, err)
		}
		if path.Base(sourcePath) == "SKILL.md" {
			raw = []byte(rewriteForCodex(string(raw), prefix))
		}
		files[target] = raw
	}
	return nil
}

func rewriteForCodex(content, sourcePrefix string) string {
	rewritten := content
	// Rewrite /<plugin>:<command> → <plugin>-<command> for all loaded plugins.
	plugins, _ := Plugins()
	for _, p := range plugins {
		re := regexp.MustCompile(`/` + regexp.QuoteMeta(p.Name) + `:([a-z-]+)`)
		rewritten = re.ReplaceAllString(rewritten, p.Name+"-$1")
	}
	rewritten = superpowersRef.ReplaceAllString(rewritten, "$1")
	if sourcePrefix != "" {
		rewritten = strings.ReplaceAll(rewritten, sourcePrefix, "")
	}
	return rewritten
}
