package cli

import (
	"fmt"
	"os"

	"github.com/donovan-yohan/belayer/internal/config"
)

// resolveInstanceName returns the instance name to use. If instanceName is
// already set it is returned as-is; otherwise the default instance from the
// global config is returned. An error is returned when no instance can be
// determined.
func resolveInstanceName(instanceName string) (string, error) {
	if instanceName != "" {
		return instanceName, nil
	}
	if envName := os.Getenv("BELAYER_INSTANCE"); envName != "" {
		return envName, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	if cfg.DefaultInstance == "" {
		return "", fmt.Errorf("no default instance set; use --instance or run `belayer instance create` first")
	}
	return cfg.DefaultInstance, nil
}
