package sandbox_test

import (
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/sandbox"
)

func TestDefaultRegistryResolvesNoop(t *testing.T) {
	d, err := sandbox.Default.Get("noop")
	if err != nil {
		t.Fatalf("Default.Get(\"noop\") returned error: %v", err)
	}
	if d == nil {
		t.Fatal("Default.Get(\"noop\") returned nil driver")
	}
	if _, ok := d.(*sandbox.Noop); !ok {
		t.Errorf("Default.Get(\"noop\") returned %T, want *sandbox.Noop", d)
	}
}

func TestDefaultRegistryReturnsErrorForUnknownName(t *testing.T) {
	_, err := sandbox.Default.Get("does-not-exist")
	if err == nil {
		t.Fatal("Default.Get(\"does-not-exist\") returned nil error, want not-registered error")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error %q does not mention \"not registered\"", err.Error())
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error %q does not name the missing driver", err.Error())
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := &sandbox.Registry{}
	reg.Register("fake", &sandbox.Noop{})

	d, err := reg.Get("fake")
	if err != nil {
		t.Fatalf("Get(\"fake\") returned error: %v", err)
	}
	if d == nil {
		t.Fatal("Get(\"fake\") returned nil driver")
	}
}

func TestRegistryNilGetReturnsError(t *testing.T) {
	var reg *sandbox.Registry
	_, err := reg.Get("anything")
	if err == nil {
		t.Fatal("nil Registry Get returned nil error, want not-registered error")
	}
}

func TestRegistryEmptyGetReturnsError(t *testing.T) {
	reg := &sandbox.Registry{}
	_, err := reg.Get("anything")
	if err == nil {
		t.Fatal("empty Registry Get returned nil error, want not-registered error")
	}
}

func TestRegistryRegisterEmptyNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with empty name did not panic")
		}
	}()
	reg := &sandbox.Registry{}
	reg.Register("", &sandbox.Noop{})
}

func TestRegistryRegisterNilDriverPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with nil driver did not panic")
		}
	}()
	reg := &sandbox.Registry{}
	reg.Register("fake", nil)
}

