package runner

import (
	"strings"
	"testing"
)

func TestBuildShim_ContainsTestFile(t *testing.T) {
	shim, err := buildShim("/path/to/my_spec.lua")
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	got := string(shim)
	if !strings.Contains(got, "/path/to/my_spec.lua") {
		t.Errorf("shim missing test file path:\n%s", got)
	}
	if !strings.Contains(got, "dofile(") {
		t.Errorf("shim missing dofile call:\n%s", got)
	}
	if !strings.Contains(got, "_neospec_report()") {
		t.Errorf("shim missing _neospec_report() call:\n%s", got)
	}
}

func TestBuildShim_EscapesBackslashes(t *testing.T) {
	// Windows-style paths contain backslashes that must be escaped.
	shim, err := buildShim(`C:\Users\test\spec.lua`)
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	got := string(shim)
	if strings.Contains(got, `C:\Users`) {
		t.Errorf("backslashes were not escaped:\n%s", got)
	}
}

func TestBuildShim_NonEmpty(t *testing.T) {
	shim, err := buildShim("spec.lua")
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	if len(shim) == 0 {
		t.Error("buildShim() returned empty shim")
	}
}
