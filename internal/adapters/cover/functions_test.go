package cover

import (
	"os"
	"strings"
	"testing"
)

// names collects the Name field from a slice of FunctionInfo for
// assertions that only care about naming, not line positions.
func names(fs []FunctionInfo) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}

func TestExtractFunctionsLocalFunctionForm(t *testing.T) {
	// local function foo() end — one of the six patterns.
	fs, err := ExtractFunctions("t.lua", []byte("local function foo() end"))
	if err != nil {
		t.Fatalf("ExtractFunctions: %v", err)
	}
	if len(fs) != 1 || fs[0].Name != "foo" {
		t.Errorf("got %v, want [foo]", names(fs))
	}
}

func TestExtractFunctionsDottedGlobalDecl(t *testing.T) {
	// function M.foo() end — dotted global declaration.
	fs, _ := ExtractFunctions("t.lua", []byte("function M.foo() end"))
	if len(fs) != 1 || fs[0].Name != "M.foo" {
		t.Errorf("got %v, want [M.foo]", names(fs))
	}
}

func TestExtractFunctionsMethodDecl(t *testing.T) {
	// function obj:greet(msg) end — colon method definition.
	fs, _ := ExtractFunctions("t.lua", []byte("function obj:greet(msg) end"))
	if len(fs) != 1 || fs[0].Name != "obj:greet" {
		t.Errorf("got %v, want [obj:greet]", names(fs))
	}
}

func TestExtractFunctionsDeepDottedChain(t *testing.T) {
	// function a.b.c.d() end — deeper than one dot; a case the shipped
	// patterns *do* handle but is worth pinning explicitly since the
	// AST walk uses a different code path.
	fs, _ := ExtractFunctions("t.lua", []byte("function a.b.c.d() end"))
	if len(fs) != 1 || fs[0].Name != "a.b.c.d" {
		t.Errorf("got %v, want [a.b.c.d]", names(fs))
	}
}

func TestExtractFunctionsLocalAssignToFunction(t *testing.T) {
	// local foo = function() end — value-slot function definition.
	fs, _ := ExtractFunctions("t.lua", []byte("local foo = function() end"))
	if len(fs) != 1 || fs[0].Name != "foo" {
		t.Errorf("got %v, want [foo]", names(fs))
	}
}

func TestExtractFunctionsGlobalAssignToFunction(t *testing.T) {
	// M.foo = function() end — assignment to a field.
	fs, _ := ExtractFunctions("t.lua", []byte("M.foo = function() end"))
	if len(fs) != 1 || fs[0].Name != "M.foo" {
		t.Errorf("got %v, want [M.foo]", names(fs))
	}
}

func TestExtractFunctionsMultiAssignToFunctions(t *testing.T) {
	// `local f, g = function() end, function() end` — the pattern-based
	// runtime misses this: two functions on distinct expressions, both
	// with real names, both currently rendered as anonymous by the
	// single-line pattern approach when the values are on the same line
	// as the local. Extractor gets both.
	fs, _ := ExtractFunctions("t.lua",
		[]byte("local f, g = function() end, function() end"))
	if len(fs) != 2 {
		t.Fatalf("got %d functions, want 2", len(fs))
	}
	got := names(fs)
	if got[0] != "f" || got[1] != "g" {
		t.Errorf("got %v, want [f g]", got)
	}
}

func TestExtractFunctionsTableFieldFunctionShorthand(t *testing.T) {
	// { foo = function() end } — table-literal method field. The
	// NAME_PATTERNS approach falls through to "anonymous@N" for this
	// shape; the AST walk propagates KeyName as the hint so the
	// function gets its real name.
	fs, _ := ExtractFunctions("t.lua",
		[]byte("local M = { foo = function() end, bar = function() end }"))
	got := names(fs)
	// Order in the output matches source order of table fields.
	if len(got) != 2 || got[0] != "foo" || got[1] != "bar" {
		t.Errorf("got %v, want [foo bar]", got)
	}
}

func TestExtractFunctionsMultiLineSignatureStillNamed(t *testing.T) {
	// The pattern-based approach reads only the definition line; a
	// signature that wraps across lines silently becomes "anonymous@N".
	// The parser sees the whole signature regardless of line breaks.
	src := []byte(`function M.multi(
    a,
    b,
    c
) return a + b + c end`)
	fs, _ := ExtractFunctions("t.lua", src)
	if len(fs) != 1 || fs[0].Name != "M.multi" {
		t.Errorf("got %v, want [M.multi]", names(fs))
	}
}

func TestExtractFunctionsAnonymousCallbackGetsPositionalLabel(t *testing.T) {
	// A function passed as a call argument has no local name. Label it
	// by position so distinct callbacks on distinct lines do not collide.
	src := []byte("\nregister(function() return 1 end)")
	fs, _ := ExtractFunctions("t.lua", src)
	if len(fs) != 1 {
		t.Fatalf("got %d functions, want 1", len(fs))
	}
	if !strings.HasPrefix(fs[0].Name, "anonymous@") {
		t.Errorf("got %q, want a positional anonymous label", fs[0].Name)
	}
	if fs[0].Line != 2 {
		t.Errorf("line = %d, want 2", fs[0].Line)
	}
}

func TestExtractFunctionsNestedDefinitions(t *testing.T) {
	// Nested functions must all be recorded — the runtime hook attributes
	// hits by prototype, so every nested closure is a separate function
	// to name. Order is outer first, then inner (walk order).
	src := []byte(`
local function outer()
    local function inner()
        return 1
    end
    return inner
end`)
	fs, _ := ExtractFunctions("t.lua", src)
	got := names(fs)
	if len(got) != 2 || got[0] != "outer" || got[1] != "inner" {
		t.Errorf("got %v, want [outer inner]", got)
	}
}

func TestExtractFunctionsLineForDeclaration(t *testing.T) {
	// The Line must match the definition's start position so the runtime
	// can look up the name by debug.getinfo(...).linedefined. Realistic
	// Lua keeps `function` on the same line as `local` / the dotted
	// prefix, so those cases give the expected line unambiguously.
	src := []byte(`

local function foo() end
function bar() end`)
	fs, _ := ExtractFunctions("t.lua", src)
	if len(fs) != 2 {
		t.Fatalf("got %d functions, want 2", len(fs))
	}
	// Line 3 (blank, blank, local function foo) and line 4 (function bar).
	if fs[0].Line != 3 || fs[1].Line != 4 {
		t.Errorf("lines = %d,%d, want 3,4", fs[0].Line, fs[1].Line)
	}
}

func TestExtractFunctionsEmpty(t *testing.T) {
	fs, _ := ExtractFunctions("t.lua", []byte("local x = 1"))
	if len(fs) != 0 {
		t.Errorf("got %d functions, want 0", len(fs))
	}
}

func TestExtractFunctionsPropagatesParseError(t *testing.T) {
	_, err := ExtractFunctions("t.lua", []byte(`"unterminated`))
	if err == nil {
		t.Error("expected parse error")
	}
}

func TestExtractFunctionsPartialAfterRecovery(t *testing.T) {
	// Even with a broken first statement, functions defined after the
	// recovery sync point should still be found.
	src := []byte("xx yy\nlocal function ok() end")
	fs, err := ExtractFunctions("t.lua", []byte(src))
	if err == nil {
		t.Fatal("expected parse error")
	}
	if len(fs) != 1 || fs[0].Name != "ok" {
		t.Errorf("got %v, want [ok]", names(fs))
	}
}

func TestExtractFunctionsIndexTargetIsAnonymous(t *testing.T) {
	// `t[1] = function() end` has no name-shaped target. renderTarget
	// returns "" so the function is labelled anonymous by position.
	fs, _ := ExtractFunctions("t.lua", []byte("t[1] = function() end"))
	if len(fs) != 1 {
		t.Fatalf("got %d functions, want 1", len(fs))
	}
	if !strings.HasPrefix(fs[0].Name, "anonymous@") {
		t.Errorf("got %q, want anonymous positional label", fs[0].Name)
	}
}

func TestExtractFunctionsCorpusNeospecLua(t *testing.T) {
	// Acceptance: neospec's own harness parses cleanly and most
	// function definitions get real (non-anonymous) names, because the
	// harness uses well-formed declarations. If real functions start
	// showing up as anonymous, either the harness added an exotic
	// definition shape or the extractor missed a case.
	src, err := os.ReadFile("../runner/lua/harness.lua")
	if err != nil {
		t.Skipf("corpus not readable: %v", err)
	}
	fs, err := ExtractFunctions("harness.lua", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fs) == 0 {
		t.Fatal("harness.lua defines functions but extractor found none")
	}
	anon := 0
	for _, f := range fs {
		if strings.HasPrefix(f.Name, "anonymous@") {
			anon++
		}
	}
	// A handful of anonymous entries is expected — callbacks passed
	// inline to pcall/xpcall and similar. Just verify most functions
	// got real names.
	if anon > len(fs)/2 {
		t.Errorf("%d of %d functions anonymous — extractor likely missing shapes", anon, len(fs))
	}
}
