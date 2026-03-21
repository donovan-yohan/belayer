package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteHooksConfig writes a .belayer/hooks.json file that configures the Stop hook
// to call `belayer node-complete` with the task ID, node name, and attempt number.
func WriteHooksConfig(workDir, taskID, nodeName string, attempt int) error {
	hooksDir := filepath.Join(workDir, ".belayer")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("create .belayer dir: %w", err)
	}

	hooksJSON := fmt.Sprintf(`{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "belayer node-complete --task-id %s --node %s --attempt %d"
          }
        ]
      }
    ]
  }
}`, taskID, nodeName, attempt)

	path := filepath.Join(hooksDir, "hooks.json")
	return os.WriteFile(path, []byte(hooksJSON), 0o644)
}

// HooksConfigPath returns the path to the hooks config file.
func HooksConfigPath(workDir string) string {
	return filepath.Join(workDir, ".belayer", "hooks.json")
}
