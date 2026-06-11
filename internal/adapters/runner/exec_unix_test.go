//go:build unix

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestRealCommandRunner_ReapsGrandchildren verifies that the runner kills the
// child's entire process group after the direct child exits. It models the real
// failure: Neovim (the direct child) exits promptly while a grandchild it
// spawned (e.g. a git clone started by lazy.nvim) keeps running and writing into
// the sandbox. Without process-group reaping, that grandchild survives and
// races the sandbox RemoveAll, producing "directory not empty".
func TestRealCommandRunner_ReapsGrandchildren(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-group reap integration test in -short mode")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}

	dir := t.TempDir()
	leaked := filepath.Join(dir, "leaked")

	// Background a grandchild that waits, then writes the marker file, while the
	// direct child (sh) exits immediately. Its stdio is redirected to /dev/null
	// so it does NOT hold the inherited stdout pipe open — otherwise cmd.Wait()
	// would block until the grandchild finished, masking the orphan race. This
	// models a real detached writer (e.g. a git clone whose output a plugin
	// manager redirects). If left running it creates `leaked` ~1s from now; if
	// its process group is reaped, it never gets the chance.
	script := fmt.Sprintf("( sleep 1; : > '%s' ) </dev/null >/dev/null 2>&1 & exit 0", leaked)

	ctx := context.Background()
	if _, _, runErr := (realCommandRunner{}).Run(ctx, nil, sh, "-c", script); runErr != nil {
		t.Fatalf("Run() returned error: %v", runErr)
	}

	// Wait well past the grandchild's sleep so a surviving process would have
	// written the file by now.
	time.Sleep(1500 * time.Millisecond)

	if _, statErr := os.Stat(leaked); !os.IsNotExist(statErr) {
		t.Fatalf("grandchild survived process-group reap and wrote %s (stat err: %v)", leaked, statErr)
	}
}
