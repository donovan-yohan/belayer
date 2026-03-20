package cli

import (
	"os"
	"path/filepath"
)

// resolveChannelPaths finds the channel script and hooks directory.
// Looks relative to the CWD (development), then in ~/.belayer/channel/ (installed).
func resolveChannelPaths() (channelScript, hooksDir string) {
	// Check relative to CWD (development).
	if _, err := os.Stat("channel/channel.ts"); err == nil {
		abs, _ := filepath.Abs("channel/channel.ts")
		hooksAbs, _ := filepath.Abs("channel/hooks")
		return abs, hooksAbs
	}

	// Check in ~/.belayer/channel/ (installed).
	home, _ := os.UserHomeDir()
	installed := filepath.Join(home, ".belayer", "channel", "channel.ts")
	if _, err := os.Stat(installed); err == nil {
		return installed, filepath.Join(home, ".belayer", "channel", "hooks")
	}

	return "", ""
}
