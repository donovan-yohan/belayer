package plugins

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	belayerassets "github.com/donovan-yohan/belayer"
)

type CodexInstallResult struct {
	Installed   bool
	StablePath  string
	VersionPath string
	Mode        string
}

// InstallCodexSkillPack writes the Belayer Codex skill pack into the Belayer
// home directory and mounts it into ~/.agents/skills/belayer when Codex is
// available on PATH. If Codex is not installed, it returns Installed=false and
// no error.
func InstallCodexSkillPack(belayerDir string) (*CodexInstallResult, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		if errorsIsNotFound(err) {
			return &CodexInstallResult{Installed: false}, nil
		}
		return nil, fmt.Errorf("looking up codex: %w", err)
	}

	skillsRoot, err := agentsSkillsDir()
	if err != nil {
		return nil, err
	}

	versionPath := filepath.Join(belayerDir, "agent-assets", "codex", belayerassets.CodexPackVersion(), "skills")
	if err := writeCodexSkillFiles(versionPath); err != nil {
		return nil, err
	}

	stablePath := filepath.Join(skillsRoot, marketplaceName)
	if err := os.MkdirAll(filepath.Dir(stablePath), 0o755); err != nil {
		return nil, fmt.Errorf("create skills root %s: %w", filepath.Dir(stablePath), err)
	}
	if err := os.RemoveAll(stablePath); err != nil {
		return nil, fmt.Errorf("remove existing skill mount %s: %w", stablePath, err)
	}

	if err := os.Symlink(versionPath, stablePath); err == nil {
		return &CodexInstallResult{
			Installed:   true,
			StablePath:  stablePath,
			VersionPath: versionPath,
			Mode:        "symlink",
		}, nil
	}

	if err := copyDir(versionPath, stablePath); err != nil {
		return nil, fmt.Errorf("copy codex skill pack to %s: %w", stablePath, err)
	}
	return &CodexInstallResult{
		Installed:   true,
		StablePath:  stablePath,
		VersionPath: versionPath,
		Mode:        "copy",
	}, nil
}

func writeCodexSkillFiles(versionPath string) error {
	files, err := belayerassets.CodexSkillFiles()
	if err != nil {
		return fmt.Errorf("generate codex skill files: %w", err)
	}
	return belayerassets.WriteSkillFiles(versionPath, files)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, current)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func agentsSkillsDir() (string, error) {
	if dir := os.Getenv("AGENTS_SKILLS_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory for Codex skills: %w", err)
	}
	return filepath.Join(home, ".agents", "skills"), nil
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}
