package cli

import (
	"testing"
)

func TestResolveCragName_EnvFallback(t *testing.T) {
	// When --crag flag is set, it wins
	name, err := resolveCragName("from-flag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "from-flag" {
		t.Errorf("expected 'from-flag', got %q", name)
	}

	// When flag is empty but BELAYER_CRAG is set, use env
	t.Setenv("BELAYER_CRAG", "from-env")
	name, err = resolveCragName("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "from-env" {
		t.Errorf("expected 'from-env', got %q", name)
	}
}
