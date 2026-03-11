package cli

import (
	"fmt"
	"os"

	"github.com/donovan-yohan/belayer/internal/config"
)

// resolveCragName returns the crag name to use. If cragName is
// already set it is returned as-is; otherwise the default crag from the
// global config is returned. An error is returned when no crag can be
// determined.
func resolveCragName(cragName string) (string, error) {
	if cragName != "" {
		return cragName, nil
	}
	if envName := os.Getenv("BELAYER_CRAG"); envName != "" {
		return envName, nil
	}
	if envName := os.Getenv("BELAYER_INSTANCE"); envName != "" {
		return envName, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	if cfg.DefaultCrag == "" {
		return "", fmt.Errorf("no default crag set; use --crag or run `belayer crag create` first")
	}
	return cfg.DefaultCrag, nil
}
