package runner

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The other Lua tests in this package assert on the embedded source text.
// That is fine for checking a function exists, but it cannot verify that the
// executable-line walk produces the right lines -- the whole point of the
// coverage fix. This test executes the hook under a real Neovim and asserts
// on the resulting coverage table.
//
// Skips when no nvim is on PATH so the suite still runs in bare environments.
func TestCoverageHook_RecordsUnexecutedLines(t *testing.T) {
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim not on PATH; skipping hook execution test")
	}

	dir := t.TempDir()

	hookSrc, err := CoverageHookSource()
	if err != nil {
		t.Fatalf("CoverageHookSource: %v", err)
	}
	hookPath := filepath.Join(dir, "coverage_hook.lua")
	if err := os.WriteFile(hookPath, hookSrc, 0o644); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	// called() runs; never_called() does not. Its body lines must appear with
	// a zero count, which is precisely what the pre-fix hook could not do.
	subject := filepath.Join(dir, "subject.lua")
	subjectSrc := `local M = {}

function M.called(x)
  local a = x + 1
  return a
end

function M.never_called(y)
  local b = y * 2
  local c = b + 3
  return c
end

return M
`
	if err := os.WriteFile(subject, []byte(subjectSrc), 0o644); err != nil {
		t.Fatalf("write subject: %v", err)
	}

	out := filepath.Join(dir, "cov.json")
	driver := filepath.Join(dir, "driver.lua")
	driverSrc := `
_neospec_coverage_include = { "` + dir + `" }
dofile("` + hookPath + `")
local M = dofile("` + subject + `")
M.called(1)
-- Tolerate the function being absent so a hook without the executable-line
-- walk still writes output: the test then fails on the missing zero-count
-- line, which names the actual defect, rather than on a missing file.
if type(_neospec_coverage_finalize) == "function" then
  _neospec_coverage_finalize()
end
local f = io.open("` + out + `", "w")
local parts = {}
for path, lines in pairs(_neospec_coverage) do
  local nums = {}
  for line, hits in pairs(lines) do
    nums[#nums+1] = string.format('"%d":%d', line, hits)
  end
  parts[#parts+1] = string.format('"%s":{%s}', path, table.concat(nums, ","))
end
f:write("{" .. table.concat(parts, ",") .. "}")
f:close()
`
	if err := os.WriteFile(driver, []byte(driverSrc), 0o644); err != nil {
		t.Fatalf("write driver: %v", err)
	}

	cmd := exec.Command(nvim, "--headless", "-u", "NONE", "-c", "luafile "+driver, "+q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nvim run failed: %v\n%s", err, b)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read coverage output: %v", err)
	}
	var cov map[string]map[string]int
	if err := json.Unmarshal(raw, &cov); err != nil {
		t.Fatalf("parse coverage %q: %v", raw, err)
	}

	var lines map[string]int
	for path, l := range cov {
		if strings.HasSuffix(path, "subject.lua") {
			lines = l
		}
	}
	if lines == nil {
		t.Fatalf("subject.lua absent from coverage: %v", cov)
	}

	// Body of the function that ran.
	if got := lines["4"]; got < 1 {
		t.Errorf("line 4 (executed) = %d hits, want >= 1", got)
	}
	// Body of the function that never ran: present, and zero.
	for _, ln := range []string{"9", "10", "11"} {
		hits, ok := lines[ln]
		if !ok {
			t.Errorf("line %s (unexecuted body) missing from coverage — the "+
				"executable-line walk did not reach it", ln)
			continue
		}
		if hits != 0 {
			t.Errorf("line %s = %d hits, want 0", ln, hits)
		}
	}
}
