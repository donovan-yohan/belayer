package cli

import (
	"testing"
)

func TestResolveInstanceName_EnvFallback(t *testing.T) {
	// When --instance flag is set, it wins
	name, err := resolveInstanceName("from-flag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "from-flag" {
		t.Errorf("expected 'from-flag', got %q", name)
	}

	// When flag is empty but BELAYER_INSTANCE is set, use env
	t.Setenv("BELAYER_INSTANCE", "from-env")
	name, err = resolveInstanceName("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "from-env" {
		t.Errorf("expected 'from-env', got %q", name)
	}
}
