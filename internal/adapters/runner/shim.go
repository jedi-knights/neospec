package runner

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed lua/*.lua
var luaFS embed.FS

// buildShim constructs the Lua entry-point that is written into the sandbox
// before each Neovim invocation. It concatenates the coverage hook and the
// test harness, then appends the dofile() call for the actual test file.
func buildShim(testFile string) ([]byte, error) {
	// The three error branches below are structurally unreachable. The //go:embed
	// directive above causes a compile-time error if any of the named Lua files
	// are missing from the source tree, so by the time the binary runs the files
	// are guaranteed present in luaFS. embed.FS.ReadFile only fails for absent
	// paths; the error returns are kept for API correctness only.
	hook, err := luaFS.ReadFile("lua/coverage_hook.lua")
	if err != nil {
		return nil, fmt.Errorf("reading coverage_hook.lua: %w", err)
	}

	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		return nil, fmt.Errorf("reading harness.lua: %w", err)
	}

	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		return nil, fmt.Errorf("reading reporter.lua: %w", err)
	}

	// Escape the test file path for embedding in a Lua string literal.
	escaped := strings.ReplaceAll(testFile, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)

	shim := string(hook) + "\n" +
		string(harness) + "\n" +
		string(reporter) + "\n" +
		fmt.Sprintf(`dofile("%s")`+"\n", escaped) +
		"_neospec_report()\n"

	return []byte(shim), nil
}
