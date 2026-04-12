package runner

import (
	"strings"
	"testing"
)

func TestBuildShim_ContainsTestFile(t *testing.T) {
	shim, err := buildShim("/path/to/my_spec.lua", "", nil)
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
	// Anchor on debug.sethook — the coverage hook's installation call — so that
	// a refactor that drops the embedded coverage_hook.lua fails explicitly rather
	// than silently producing a shim without coverage instrumentation.
	if !strings.Contains(got, "debug.sethook") {
		t.Errorf("shim missing debug.sethook (coverage hook content):\n%s", got)
	}
}

func TestBuildShim_EscapesBackslashes(t *testing.T) {
	// Windows-style paths contain backslashes that must be escaped.
	shim, err := buildShim(`C:\Users\test\spec.lua`, "", nil)
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	got := string(shim)
	if strings.Contains(got, `C:\Users`) {
		t.Errorf("backslashes were not escaped:\n%s", got)
	}
	// The escaped form must be present — a faulty escaper might strip
	// backslashes entirely and still pass the absence check above.
	if !strings.Contains(got, `C:\\Users`) {
		t.Errorf("escaped backslash sequence not found in shim:\n%s", got)
	}
}

func TestBuildShim_NonEmpty(t *testing.T) {
	shim, err := buildShim("spec.lua", "", nil)
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	if len(shim) == 0 {
		t.Error("buildShim() returned empty shim")
	}
}

func TestBuildShim_WithInitFile(t *testing.T) {
	shim, err := buildShim("/tests/my_spec.lua", "/tests/minimal_init.lua", nil)
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	got := string(shim)

	// init file dofile must appear before the coverage hook.
	// Anchor on debug.sethook — the API call inside coverage_hook.lua — rather
	// than the filename string, so a rename of coverage_hook.lua does not silently
	// break this test with a misleading error message.
	initPos := strings.Index(got, `dofile("/tests/minimal_init.lua")`)
	hookPos := strings.Index(got, "debug.sethook")
	if initPos == -1 {
		t.Fatalf("shim missing dofile for init file:\n%s", got)
	}
	if hookPos == -1 {
		t.Fatalf("shim missing debug.sethook (coverage hook) content:\n%s", got)
	}
	if initPos > hookPos {
		t.Errorf("init file dofile (%d) must appear before coverage hook (%d)", initPos, hookPos)
	}
}

func TestBuildShim_NoInitFile(t *testing.T) {
	shim, err := buildShim("/tests/my_spec.lua", "", nil)
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	got := string(shim)
	// When no init file is given, the shim must NOT contain a bare dofile("") call.
	if strings.Contains(got, `dofile("")`) {
		t.Errorf("shim should not contain dofile(\"\") when initFile is empty:\n%s", got)
	}
}

// TestBuildShim_EscapesNewlines verifies that path characters requiring Lua
// string escaping (newline, tab, carriage return) are escaped in the shim.
// An unescaped newline inside a dofile("...") argument is a Lua syntax error.
func TestBuildShim_EscapesNewlines(t *testing.T) {
	shim, err := buildShim("/tmp/test\nfile.lua", "", nil)
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	got := string(shim)
	// A literal newline inside the dofile argument is invalid Lua.
	if strings.Contains(got, "dofile(\"/tmp/test"+"\n"+"file") {
		t.Error("buildShim() left literal newline in dofile path — would produce broken Lua")
	}
	// The escaped two-character sequence \n must appear instead.
	if !strings.Contains(got, `dofile("/tmp/test\nfile.lua")`) {
		t.Error("buildShim() did not emit escaped \\n sequence for newline in path")
	}
}

// TestHarness_AssertMethods verifies that harness.lua defines all assert methods
// expected by Plenary/Busted-style test suites. These are required for
// compatibility with external plugin test suites (e.g. yoda.nvim) that use the
// full busted assert API.
func TestHarness_AssertMethods(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)

	required := []string{
		"assert.same",
		"assert.is_table",
		"assert.truthy",
		"assert.is_truthy",
		"assert.is_function",
		"assert.is_string",
		"assert.is_boolean",
	}
	for _, method := range required {
		if !strings.Contains(got, method) {
			t.Errorf("harness.lua missing %s", method)
		}
	}
}

// TestHarness_UsesVimUv verifies that harness.lua uses vim.uv (not the
// deprecated vim.loop) for the high-resolution timer.
func TestHarness_UsesVimUv(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if strings.Contains(got, "vim.loop") {
		t.Error("harness.lua uses deprecated vim.loop; replace with vim.uv")
	}
}

// TestHarness_ResultsGuarded verifies that _neospec_results is initialised with
// a type check so that both nil/false and truthy non-table values are repaired.
// The `or {}` shorthand only fixes nil/false; a truthy non-table (e.g. the
// number 1) bypasses it and causes table.insert to crash on the next result.
func TestHarness_ResultsGuarded(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, `type(_neospec_results) ~= "table"`) {
		t.Error(`harness.lua initialises _neospec_results without a type guard; ` +
			`use 'if type(_neospec_results) ~= "table" then _neospec_results = {} end' ` +
			`to guard against both nil and truthy non-table values`)
	}
}

// TestReporter_ArrayDetectionUsesExplicitCount verifies that reporter.lua's
// array detector uses an explicit entry count rather than the # operator.
// The # operator is undefined on non-contiguous (holey) integer tables, such
// as coverage line maps where blank lines and comments create gaps between
// recorded line numbers. Using # can produce false-positive array detections
// and silently drop coverage entries when a gap causes #v < max.
func TestReporter_ArrayDetectionUsesExplicitCount(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	if strings.Contains(got, "max == #v") {
		t.Error("reporter.lua uses `max == #v` for array detection; " +
			"the # operator is undefined on holey tables and can silently drop coverage line entries")
	}
}

// TestReporter_NaNGuard verifies that reporter.lua guards against non-finite
// Lua numbers (math.huge, -math.huge, 0/0) that produce invalid JSON literals
// when passed through tostring().
func TestReporter_NaNGuard(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	// The guard should check for NaN (v ~= v) and infinity before serialising.
	if !strings.Contains(got, "math.huge") {
		t.Error("reporter.lua json_value does not guard against math.huge / non-finite numbers")
	}
}

// TestHarness_IsTruthyIsWrapper verifies that assert.is_truthy is defined as a
// wrapper function rather than a snapshot alias of assert.truthy. A snapshot
// alias (assert.is_truthy = assert.truthy) would silently diverge if
// assert.truthy is ever monkeypatched, breaking Plenary/Busted compatibility.
func TestHarness_IsTruthyIsWrapper(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if strings.Contains(got, "assert.is_truthy = assert.truthy") {
		t.Error(`harness.lua uses a snapshot alias for assert.is_truthy; define it as a wrapper function instead`)
	}
	if !strings.Contains(got, "function assert.is_truthy(") {
		t.Error("harness.lua does not define assert.is_truthy as a function")
	}
}

// TestHarness_SameHasCycleGuard verifies that assert.same's deep_equal guards
// against self-referential tables. Without a cycle guard, circular tables cause
// a stack overflow reported as "stack overflow" rather than a clear failure.
func TestHarness_SameHasCycleGuard(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "seen") {
		t.Error("harness.lua assert.same deep_equal has no cycle guard (no 'seen' table)")
	}
}

// TestHarness_AssertInitialisedAsTable verifies that harness.lua initialises
// assert as a table regardless of whether Lua's built-in assert function is
// still in scope. The built-in assert is a truthy non-table value, so the
// pattern "assert = assert or {}" silently retains the function and causes
// "attempt to index global 'assert'" errors at runtime.
func TestHarness_AssertInitialisedAsTable(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	// The guard must check the type, not rely on truthiness.
	if strings.Contains(got, "assert = assert or {}") {
		t.Error(`harness.lua uses "assert = assert or {}" which retains Lua's built-in assert function; ` +
			`use "if type(assert) ~= \"table\" then assert = {} end" instead`)
	}
}

// TestHarness_LoadGuard verifies that harness.lua guards against double-load
// by checking the _neospec_harness_loaded flag. Without this guard a second
// source of the file redefines all DSL globals (describe, it, before_each,
// after_each, pending), discarding any monkey-patches the test file applied.
func TestHarness_LoadGuard(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "_neospec_harness_loaded") {
		t.Error("harness.lua missing double-load guard (_neospec_harness_loaded)")
	}
}

// TestHarness_CycleGuardIsSymmetric verifies that assert.same's deep_equal
// records both directions of a table-pair in the cycle guard. Only recording
// the forward direction (seen[a][b]) causes the recursion to go one level deeper
// on mutual-reference structures (A.x=B, B.x=A) before detecting the cycle,
// increasing stack depth on complex graphs. Recording both directions is the
// correct approach and is safe: the reverse entry can only short-circuit a
// (b,a) call to true after (a,b) has already been entered, which only happens
// during cycle traversal where returning true is correct.
func TestHarness_CycleGuardIsSymmetric(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "seen[b][a] = true") {
		t.Error("harness.lua cycle guard missing symmetric (b→a) entry; " +
			"both seen[a][b] and seen[b][a] must be set to avoid extra recursion depth on cyclic structures")
	}
}

// TestReporter_LoadGuard verifies that reporter.lua guards against double-load.
// Without a guard, sourcing reporter.lua a second time redefines _neospec_report
// and the new function closes over fresh local copies of json_string/json_value.
// More critically, if a test shim calls _neospec_report twice (e.g. via a buggy
// init_file that re-sources this file), stdout receives two JSON payloads and
// the Go consumer fails to parse the output.
func TestReporter_LoadGuard(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	if !strings.Contains(got, "_neospec_report_loaded") {
		t.Error("reporter.lua missing double-load guard (_neospec_report_loaded)")
	}
}

// TestHarness_AssertPreservesCallability verifies that harness.lua adds a
// __call metamethod to the assert table so that bare assert(v, msg) calls
// continue to work after harness.lua replaces the built-in assert function.
// Without this, any test file that uses assert(condition) rather than
// assert.equals(a, b) would raise "attempt to call a table value (global 'assert')".
func TestHarness_AssertPreservesCallability(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "__call") {
		t.Error("harness.lua does not add __call metamethod to assert table; " +
			"bare assert(v) calls will fail at runtime")
	}
}

// TestHarness_BuiltinAssertCapturedBeforeConditional verifies that
// _builtin_assert is captured BEFORE the if/else type-check block so that
// both the "assert is a function" and "assert is a table" branches have access
// to it. If captured inside the "if" block, the "else" branch cannot use it,
// which means the __call metamethod on the else-path references an undefined
// upvalue and fails at runtime.
func TestHarness_BuiltinAssertCapturedBeforeConditional(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	builtinIdx := strings.Index(got, "local _builtin_assert")
	ifIdx := strings.Index(got, "if type(assert) ~=")
	if builtinIdx == -1 {
		t.Fatal("harness.lua missing _builtin_assert capture")
	}
	if ifIdx == -1 {
		t.Fatal("harness.lua missing type(assert) conditional")
	}
	if builtinIdx > ifIdx {
		t.Errorf("_builtin_assert (pos %d) must be captured before the type(assert) conditional (pos %d)",
			builtinIdx, ifIdx)
	}
}

// TestHarness_BeforeAfterEachGuardOutsideDescribe verifies that before_each and
// after_each guard against being called outside a describe block. Without the
// guard, accessing _before_each_stack[0] returns nil and table.insert crashes
// with "attempt to index a nil value".
func TestHarness_BeforeAfterEachGuardOutsideDescribe(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "before_each called outside") {
		t.Error("harness.lua before_each missing guard for out-of-describe usage; " +
			"calling it outside describe indexes nil in _before_each_stack")
	}
	if !strings.Contains(got, "after_each called outside") {
		t.Error("harness.lua after_each missing guard for out-of-describe usage; " +
			"calling it outside describe indexes nil in _after_each_stack")
	}
}

// TestCoverageHook_HasDoubleLoadGuard verifies that coverage_hook.lua guards
// against double-load. Without a guard, a second source of the file resets
// _neospec_coverage to {} and discards all accumulated coverage data collected
// by the first hook installation.
func TestCoverageHook_HasDoubleLoadGuard(t *testing.T) {
	hook, err := luaFS.ReadFile("lua/coverage_hook.lua")
	if err != nil {
		t.Fatalf("reading coverage_hook.lua: %v", err)
	}
	got := string(hook)
	if !strings.Contains(got, "_neospec_coverage_loaded") {
		t.Error("coverage_hook.lua missing double-load guard (_neospec_coverage_loaded)")
	}
}

// TestReporter_CallOnceGuard verifies that _neospec_report() is guarded against
// being called more than once. A double call would emit two JSON objects to
// stdout, corrupting the stream that the Go consumer parses.
func TestReporter_CallOnceGuard(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	if !strings.Contains(got, "_report_emitted") {
		t.Error("reporter.lua _neospec_report missing call-once guard (_report_emitted)")
	}
}

// TestHarness_ItPendingGuardOutsideDescribe verifies that it() and pending() emit
// an error result when called outside any describe block rather than silently
// recording a result with an empty name. Without this guard, top-level it() calls
// produce test results with names like "> test name" (leading separator) and the
// test is attributed to no suite context, making failures impossible to locate.
func TestHarness_ItPendingGuardOutsideDescribe(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "it called outside") {
		t.Error("harness.lua it() missing guard for out-of-describe usage; " +
			"calling it() at the top level produces results with no describe context")
	}
	if !strings.Contains(got, "pending called outside") {
		t.Error("harness.lua pending() missing guard for out-of-describe usage")
	}
}

// TestHarness_DescribeCapturesNameBeforeError verifies that describe() records
// the block name using only the names accumulated up to and including the failing
// block, not residual stack state from sibling blocks. The test checks that the
// name is built from _describe_stack at the point the error is captured (i.e.
// after the name is pushed but before fn runs), not from a snapshot taken after
// the fn returns and the stack is partially unwound.
func TestHarness_DescribeCapturesNameBeforeError(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	// The error result must be recorded inside the pcall block (before the pops),
	// so the name should be captured after push and before pop. Verify the error
	// branch uses table.concat(_describe_stack) rather than a variable captured
	// before the table.insert calls — the latter would produce an empty name for
	// top-level describes.
	if !strings.Contains(got, "table.concat(_describe_stack") {
		t.Error("harness.lua describe() error branch must use table.concat(_describe_stack) " +
			"to build the error name from the live stack, not from a pre-push snapshot")
	}
}

// TestReporter_HasPcallGuard verifies that _neospec_report wraps its body in a
// pcall so that corrupted globals (_neospec_results or _neospec_coverage set to a
// non-table) do not crash the function and leave the Go consumer with no JSON.
// Without a pcall, pairs(nil) or pairs(42) raises an unrecoverable error and
// stdout receives nothing — the Go consumer silently fails with "unexpected EOF".
func TestReporter_HasPcallGuard(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	// The _neospec_report function body must be wrapped in pcall so that a
	// corrupted global cannot prevent any JSON from being written to stdout.
	if !strings.Contains(got, "pcall(function()") {
		t.Error("reporter.lua _neospec_report body is not wrapped in pcall; " +
			"corrupted globals will crash the function and produce no JSON output")
	}
}

// TestCoverageHook_HasEventTypeGuard verifies that coverage_hook.lua's hook
// function checks that the event is "line" before recording coverage. The hook
// is registered for "l" (line) events only, but if the registration changes to
// include call/return events the guard prevents those frames from inflating counts.
func TestCoverageHook_HasEventTypeGuard(t *testing.T) {
	hook, err := luaFS.ReadFile("lua/coverage_hook.lua")
	if err != nil {
		t.Fatalf("reading coverage_hook.lua: %v", err)
	}
	got := string(hook)
	if !strings.Contains(got, `event ~= "line"`) {
		t.Error("coverage_hook.lua hook() missing event-type guard; " +
			"if registration changes to include call/return events, non-line frames inflate coverage counts")
	}
}

// TestCoverageHook_HasEmptyPathGuard verifies that coverage_hook.lua guards
// against an empty path string derived from a bare "@" source. Without this
// guard, a source of exactly "@" produces an empty-string key in the coverage
// map, which the Go consumer would decode as a coverage entry with path = "".
func TestCoverageHook_HasEmptyPathGuard(t *testing.T) {
	hook, err := luaFS.ReadFile("lua/coverage_hook.lua")
	if err != nil {
		t.Fatalf("reading coverage_hook.lua: %v", err)
	}
	got := string(hook)
	if !strings.Contains(got, "#path == 0") {
		t.Error("coverage_hook.lua missing empty-path guard; " +
			"a source of exactly \"@\" would produce an empty-string key in the coverage map")
	}
}

// TestHarness_ElseBranchHasSetmetatable verifies that harness.lua's else branch
// (for when assert is already a table) calls setmetatable after vim.tbl_extend.
// vim.tbl_extend returns a new table without the source table's metatable, so
// any __call metamethod on the pre-existing assert table is silently dropped.
// Without setmetatable in the else branch, bare assert(v, msg) calls in test
// files that run after a user init_file that sets assert as a table will raise
// "attempt to call a table value (global 'assert')".
func TestHarness_ElseBranchHasSetmetatable(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	// There must be at least two setmetatable(assert, ...) calls: one in the
	// if branch (creating a new assert table for the built-in function case) and
	// one in the else branch (restoring/adding __call after vim.tbl_extend).
	count := strings.Count(got, "setmetatable(assert,")
	if count < 2 {
		t.Errorf("harness.lua has only %d setmetatable(assert,...) call(s); "+
			"the else branch must also call setmetatable to add __call after vim.tbl_extend "+
			"(vim.tbl_extend drops the source table's metatable)", count)
	}
}

// TestBuildShim_EmptyTestFile verifies that buildShim returns an error when
// testFile is empty. An empty path would produce dofile("") in the shim, which
// is a Lua runtime error ("cannot open : No such file or directory") rather
// than a clear Go error pointing at the caller.
func TestBuildShim_EmptyTestFile(t *testing.T) {
	_, err := buildShim("", "", nil)
	if err == nil {
		t.Error("buildShim(\"\", \"\") expected error for empty test file, got nil")
	}
}

// TestHarness_MatchesChecksPattern verifies that assert.matches guards against a
// non-string pattern argument. A nil or non-string pattern causes string.match
// to raise an opaque C-runtime error ("string expected, got nil") rather than a
// readable assertion failure pointing at the test file's line. The type guard
// converts the crash into a clear assertion failure at the call site.
func TestHarness_MatchesChecksPattern(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, `type(pattern) ~= "string"`) {
		t.Error("harness.lua assert.matches does not guard against non-string pattern argument; " +
			"a nil pattern raises a cryptic C-runtime error instead of a readable assertion failure")
	}
}

// TestHarness_FirstLoadResultsTypeGuard verifies that harness.lua's first-load
// path uses an explicit type check for _neospec_results rather than relying
// on `or {}`. The `or {}` pattern only repairs nil/false; a truthy non-table
// value (e.g. a number) bypasses it and causes the next table.insert to crash.
func TestHarness_FirstLoadResultsTypeGuard(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	// The first-load path (after _neospec_harness_loaded = true) must use a
	// type check, not the `or {}` shorthand that silently keeps truthy non-tables.
	// Count occurrences: double-load guard + first-load guard = at least 2.
	count := strings.Count(got, `type(_neospec_results) ~= "table"`)
	if count < 2 {
		t.Errorf("harness.lua has %d type(_neospec_results) guard(s), want at least 2; "+
			"the first-load path must also guard against a truthy non-table _neospec_results", count)
	}
}

// TestReporter_UsesPercentZForNul verifies that reporter.lua uses the Lua %z
// pattern class to match NUL bytes (U+0000) separately from the remaining
// control characters. In LuaJIT (the Lua engine Neovim embeds), a NUL byte
// inside a character-class bracket prematurely terminates the pattern string
// because patterns are processed as C strings internally. This causes
// [\0-\31] to silently match nothing for U+0000, allowing NUL bytes in test
// error messages to pass through unescaped and producing invalid JSON.
func TestReporter_UsesPercentZForNul(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	if !strings.Contains(got, "%z") {
		t.Error("reporter.lua json_string does not use %z pattern for NUL byte escaping; " +
			"the [\\0-\\31] range silently fails in LuaJIT, allowing NUL bytes to produce invalid JSON output")
	}
}

// TestHarness_DoubleLoadResultsTypeGuard verifies that harness.lua's double-load
// early-return path guards against _neospec_results being a non-table value.
// The `or {}` pattern is only reached when _neospec_results is falsy; a test
// that sets _neospec_results=nil before a second harness load would hit the
// early-return and _neospec_results = _neospec_results or {} would correctly
// repair it — but only if the type guard was added to the early-return path.
// Without an explicit type check, a non-table that is truthy (e.g. a number)
// would not be repaired by `or {}` and would crash the next table.insert call.
func TestHarness_DoubleLoadResultsTypeGuard(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, `type(_neospec_results) ~= "table"`) {
		t.Error("harness.lua double-load path missing type guard for _neospec_results; " +
			"a truthy non-table value (e.g. a number) bypasses the 'or {}' guard and crashes table.insert")
	}
}

// TestCoverageHook_LoadGuardUsesStrictTrue verifies that coverage_hook.lua checks
// _neospec_coverage_loaded == true rather than bare truthiness. A bare truthiness
// check passes for any truthy value (e.g. the integer 1), so a test that sets
// _neospec_coverage_loaded = 1 would suppress the load guard without setting the
// hook. The strict equality check ensures only the exact boolean true sentinel
// is accepted.
func TestCoverageHook_LoadGuardUsesStrictTrue(t *testing.T) {
	hook, err := luaFS.ReadFile("lua/coverage_hook.lua")
	if err != nil {
		t.Fatalf("reading coverage_hook.lua: %v", err)
	}
	got := string(hook)
	if !strings.Contains(got, "_neospec_coverage_loaded == true") {
		t.Error("coverage_hook.lua load guard uses bare truthiness check; " +
			"use '_neospec_coverage_loaded == true' to reject non-boolean truthy sentinels")
	}
}

// TestCoverageHook_HasNonCorruptionGuard verifies that coverage_hook.lua's hook
// function guards against _neospec_coverage being set to a non-table value by a
// misbehaving test. Without this guard, indexing a nil or non-table value raises
// "attempt to index a nil value (global '_neospec_coverage')", which aborts the
// hook for the rest of the run and silently discards all subsequent coverage data.
func TestCoverageHook_HasNonCorruptionGuard(t *testing.T) {
	hook, err := luaFS.ReadFile("lua/coverage_hook.lua")
	if err != nil {
		t.Fatalf("reading coverage_hook.lua: %v", err)
	}
	got := string(hook)
	if !strings.Contains(got, `type(_neospec_coverage) ~= "table"`) {
		t.Error("coverage_hook.lua missing non-table guard for _neospec_coverage; " +
			"a test that sets _neospec_coverage = nil would crash the hook and discard all coverage data")
	}
}

// TestHarness_VimUvNilGuard verifies that harness.lua checks that vim.uv is
// available before calling hrtime(). Without this guard, loading the harness on
// Neovim < 0.10 (which does not have vim.uv) produces a cryptic
// "attempt to index a nil value" crash inside it(). The guard converts that into
// a clear "neospec requires Neovim 0.10+" diagnostic at load time.
func TestHarness_VimUvNilGuard(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "if not vim.uv then") {
		t.Error("harness.lua missing vim.uv nil guard; " +
			"Neovim < 0.10 does not have vim.uv, causing a cryptic nil-index crash instead of a clear version error")
	}
}

// TestHarness_LoadGuardUsesStrictTrue verifies that harness.lua's double-load guard
// checks _neospec_harness_loaded == true rather than bare truthiness. A bare check
// accepts any truthy value; a test that sets _neospec_harness_loaded = 1 would
// suppress the guard without the harness having been properly loaded.
func TestHarness_LoadGuardUsesStrictTrue(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "_neospec_harness_loaded == true") {
		t.Error("harness.lua load guard uses bare truthiness check; " +
			"use '_neospec_harness_loaded == true' to reject non-boolean truthy sentinels")
	}
}

// TestHarness_UsesTblDeepExtend verifies that harness.lua uses vim.tbl_deep_extend
// rather than vim.tbl_extend when copying the assert table. vim.tbl_extend does a
// shallow copy, so nested tables on the original assert are shared by reference.
// vim.tbl_deep_extend copies nested tables so mutations through either reference
// cannot affect the other.
func TestHarness_UsesTblDeepExtend(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if strings.Contains(got, `vim.tbl_extend("force"`) {
		t.Error("harness.lua uses vim.tbl_extend for assert copy; " +
			"use vim.tbl_deep_extend to avoid sharing nested table references")
	}
	if !strings.Contains(got, `vim.tbl_deep_extend("force"`) {
		t.Error("harness.lua does not use vim.tbl_deep_extend for assert copy")
	}
}

// TestHarness_BeforeEachFailureScopedTeardown verifies that harness.lua tracks the
// current before_each level index so that after_each is only run for levels whose
// before_each hooks actually executed. Running all after_each levels when an outer
// before_each fails would incorrectly invoke teardown for inner levels that were
// never set up.
func TestHarness_BeforeEachFailureScopedTeardown(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, "level_idx") {
		t.Error("harness.lua before_each failure block does not use level_idx to scope teardown; " +
			"after_each must only run for levels up to and including the level whose before_each failed")
	}
}

// TestHarness_AssertEqualsUsesInspect verifies that assert.equals uses a shared
// fmt_val helper that calls vim.inspect for table values. tostring() on a table
// returns an opaque "table: 0x..." address; vim.inspect produces readable content,
// making assertion failure messages useful for debugging.
func TestHarness_AssertEqualsUsesInspect(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	// A module-local fmt_val helper must be defined to format values for assert.equals.
	if !strings.Contains(got, "local function fmt_val") {
		t.Error("harness.lua missing fmt_val helper for assert.equals; " +
			"define a module-local fmt_val that uses vim.inspect for tables")
	}
}

// TestHarness_MatchesRejectsNonString verifies that assert.matches raises a type
// error when str is not a string, rather than silently coercing it via tostring().
// Silent coercion masks test errors: if the actual value is a table, the assertion
// appears to pass whenever the table's address ("table: 0x...") matches the pattern.
func TestHarness_MatchesRejectsNonString(t *testing.T) {
	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		t.Fatalf("reading harness.lua: %v", err)
	}
	got := string(harness)
	if !strings.Contains(got, `type(str) ~= "string"`) {
		t.Error("harness.lua assert.matches does not guard against non-string str argument; " +
			"passing a table silently coerces it and may produce a false-passing assertion")
	}
}

// TestReporter_SortsObjectKeys verifies that reporter.lua sorts object keys before
// emitting them. Lua's pairs() does not guarantee stable key order, so without
// explicit sorting the JSON output is non-deterministic across runs. Deterministic
// output simplifies snapshot testing and manual diffing of coverage reports.
func TestReporter_SortsObjectKeys(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	if !strings.Contains(got, "table.sort(keys") {
		t.Error("reporter.lua json_value object encoder does not sort keys; " +
			"pairs() iteration order is non-deterministic, producing non-reproducible JSON output")
	}
}

// TestReporter_JsonValueHasDepthLimit verifies that reporter.lua's json_value
// function accepts a depth parameter and aborts at a maximum depth. Without a
// depth limit, a deeply nested or self-referential table in test output overflows
// the Lua call stack with an opaque error rather than a clear diagnostic.
func TestReporter_JsonValueHasDepthLimit(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	if !strings.Contains(got, "depth > 64") {
		t.Error("reporter.lua json_value has no depth limit; " +
			"deeply nested tables will overflow the Lua call stack instead of returning a safe sentinel")
	}
}

// TestReporter_JsonStringGuardsNil verifies that reporter.lua's json_string
// function rejects nil rather than silently coercing it to the string "nil".
// An accidental nil key in the coverage map would otherwise produce {"nil":1}
// in the JSON output instead of surfacing the bug at the point of coercion.
func TestReporter_JsonStringGuardsNil(t *testing.T) {
	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		t.Fatalf("reading reporter.lua: %v", err)
	}
	got := string(reporter)
	if !strings.Contains(got, "json_string: nil") {
		t.Error("reporter.lua json_string does not guard against nil input; " +
			"a nil key would silently produce the string \"nil\" in JSON output")
	}
}

// TestBuildShim_RejectsNULByte verifies that buildShim returns an error when
// either the test file or init file path contains a NUL byte. A NUL byte inside
// a Lua double-quoted string is not portable across Lua implementations; on
// LuaJIT (used by Neovim) it silently truncates the string, causing a
// "file not found" error rather than a clear diagnostic.
func TestBuildShim_RejectsNULByte(t *testing.T) {
	tests := []struct {
		name     string
		testFile string
		initFile string
	}{
		{"nul in testFile", "/tmp/spec\x00.lua", ""},
		{"nul in initFile", "/tmp/spec.lua", "/init\x00.lua"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildShim(tc.testFile, tc.initFile, nil)
			if err == nil {
				t.Errorf("buildShim(%q, %q) expected error for NUL byte, got nil", tc.testFile, tc.initFile)
			}
		})
	}
}

// TestBuildShim_CoverageIncludeAddsGlobal verifies that when coverageInclude
// patterns are provided, buildShim emits a _neospec_coverage_include global
// before the coverage hook so the hook can filter recorded paths.
func TestBuildShim_CoverageIncludeAddsGlobal(t *testing.T) {
	shim, err := buildShim("/path/to/spec.lua", "", []string{"lua/", "plugin/"})
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	got := string(shim)
	if !strings.Contains(got, "_neospec_coverage_include = {") {
		t.Errorf("shim missing _neospec_coverage_include assignment preamble:\n%s", got)
	}
	if !strings.Contains(got, `"lua/"`) {
		t.Errorf("shim missing pattern %q:\n%s", "lua/", got)
	}
	if !strings.Contains(got, `"plugin/"`) {
		t.Errorf("shim missing pattern %q:\n%s", "plugin/", got)
	}
	// The assignment preamble must appear before debug.sethook so the hook reads
	// the global on load rather than after it has already started running.
	globalPos := strings.Index(got, "_neospec_coverage_include = {")
	hookPos := strings.Index(got, "debug.sethook")
	if globalPos > hookPos {
		t.Errorf("_neospec_coverage_include global (%d) must appear before debug.sethook (%d)", globalPos, hookPos)
	}
}

// TestBuildShim_NoCoverageInclude_NoPreamble verifies that when no include
// patterns are given, buildShim does not emit the _neospec_coverage_include
// assignment preamble. The coverage_hook.lua reads this global but it is
// intentionally absent so the hook falls through to its default behaviour of
// recording all project sources without filtering.
func TestBuildShim_NoCoverageInclude_NoPreamble(t *testing.T) {
	shim, err := buildShim("/path/to/spec.lua", "", nil)
	if err != nil {
		t.Fatalf("buildShim() error: %v", err)
	}
	got := string(shim)
	// Check for the assignment preamble specifically — the global name itself
	// appears in coverage_hook.lua and is expected; only the preamble assignment
	// that sets the value should be absent when no patterns are given.
	if strings.Contains(got, "_neospec_coverage_include = {") {
		t.Errorf("shim must not emit _neospec_coverage_include assignment when no patterns given:\n%s", got)
	}
}

// TestCoverageHook_ChecksIncludePatterns verifies that coverage_hook.lua reads
// _neospec_coverage_include and uses it to filter which files are recorded.
// The hook must check this global in is_project_source so that paths not
// matching any include pattern are excluded from coverage data.
func TestCoverageHook_ChecksIncludePatterns(t *testing.T) {
	hook, err := luaFS.ReadFile("lua/coverage_hook.lua")
	if err != nil {
		t.Fatalf("reading coverage_hook.lua: %v", err)
	}
	got := string(hook)
	if !strings.Contains(got, "_neospec_coverage_include") {
		t.Errorf("coverage_hook.lua does not check _neospec_coverage_include — include filtering is not implemented")
	}
}
