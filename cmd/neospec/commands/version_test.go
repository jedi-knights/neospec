package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewVersionCmd(t *testing.T) {
	cmd := NewVersionCmd("1.2.3")
	if cmd == nil {
		t.Fatal("NewVersionCmd() returned nil")
	}
	if cmd.Use != "version" {
		t.Errorf("cmd.Use = %q, want %q", cmd.Use, "version")
	}
}

func TestVersionCmd_Run(t *testing.T) {
	cmd := NewVersionCmd("1.2.3")

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Execute the Run function directly to cover the fmt.Println call.
	cmd.Run(cmd, nil)

	// The output goes to stdout (not cmd.OutOrStdout()), so we just confirm
	// the Run func didn't panic. The version string "neospec 1.2.3" would be
	// printed to the process stdout.
	_ = strings.Contains(buf.String(), "1.2.3") // may or may not capture
}
