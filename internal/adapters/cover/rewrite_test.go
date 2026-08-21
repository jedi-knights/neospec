package cover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	lua "github.com/jedi-knights/go-lua-parser"
)

func TestRewriteInjectsAtIfArmBody(t *testing.T) {
	src := []byte("if x then A() end")
	out, injections, err := Rewrite("t.lua", src, RewriteOptions{})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if len(injections) != 1 {
		t.Fatalf("injections = %d, want 1", len(injections))
	}
	if !strings.Contains(string(out), "_neospec_br(1); A()") {
		t.Errorf("rewritten source missing injection:\n%s", out)
	}
}

func TestRewriteIfElseifElseArmIndicesMatchDetector(t *testing.T) {
	// The detector creates a separate 2-arm branch per decision (if +
	// each elseif), each with arms [then=0, else=1]. Every injection
	// therefore attributes to arm 0 of ITS OWN decision — except the
	// terminal else clause, which is arm 1 of the LAST decision
	// (the last elseif when present, otherwise the if).
	//
	// An earlier revision numbered arms as 0,1,2,3 (as if all bodies
	// were arms of one virtual combined branch); ApplyBranchCounters
	// would then miss the arm on every elseif and drop the else
	// counter entirely. See planIf's comment for the full story.
	src := []byte("if x then A() elseif y then B() elseif z then C() else D() end")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 4 {
		t.Fatalf("got %d injections, want 4 (then + 2 elseifs + else)", len(injections))
	}
	wantArms := []int{0, 0, 0, 1}
	for i, want := range wantArms {
		if injections[i].ArmIndex != want {
			t.Errorf("injection %d: ArmIndex = %d, want %d", i, injections[i].ArmIndex, want)
		}
	}
}

func TestRewriteIfElseifElseTargetsCorrectDecisionLine(t *testing.T) {
	// The else clause's injection must reference the LAST decision's
	// line, not the outer if's line — otherwise attribution looks up
	// (if_line, arm 1) which is a different branch than the one the
	// counter should land on.
	src := []byte("if x then\n  A()\nelseif y then\n  B()\nelse\n  C()\nend")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 3 {
		t.Fatalf("got %d injections, want 3", len(injections))
	}
	// injections[0] = then arm of if (line 1), armIdx 0
	// injections[1] = then arm of elseif (line 3), armIdx 0
	// injections[2] = else clause, arm 1 of the LAST decision (line 3)
	if injections[0].Line != 1 || injections[0].ArmIndex != 0 {
		t.Errorf("then injection = %+v, want line=1 arm=0", injections[0])
	}
	if injections[1].Line != 3 || injections[1].ArmIndex != 0 {
		t.Errorf("elseif injection = %+v, want line=3 arm=0", injections[1])
	}
	if injections[2].Line != 3 || injections[2].ArmIndex != 1 {
		t.Errorf("else injection = %+v, want line=3 arm=1 (last decision)", injections[2])
	}
}

func TestRewriteWhile(t *testing.T) {
	src := []byte("while x do A() end")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 1 || injections[0].ArmIndex != 0 {
		t.Errorf("got %+v, want single arm-0 injection", injections)
	}
}

func TestRewriteRepeat(t *testing.T) {
	src := []byte("repeat A() until x")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 1 {
		t.Errorf("got %d injections, want 1", len(injections))
	}
}

func TestRewriteNumericFor(t *testing.T) {
	src := []byte("for i = 1, 10 do A(i) end")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 1 {
		t.Errorf("got %d injections, want 1", len(injections))
	}
}

func TestRewriteGenericFor(t *testing.T) {
	src := []byte("for k, v in pairs(t) do A(k, v) end")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 1 {
		t.Errorf("got %d injections, want 1", len(injections))
	}
}

func TestRewriteEmptyBodySkipped(t *testing.T) {
	// A body with no statements and no return is a no-op — nothing to
	// count. Rewriter should skip it rather than emitting a stray
	// counter call at an ambiguous offset.
	src := []byte("if x then end")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 0 {
		t.Errorf("empty body should have no injection, got %+v", injections)
	}
}

func TestRewriteBodyWithOnlyReturn(t *testing.T) {
	// A body that has only a return still counts — Block.Return is the
	// injection point when Statements is empty. Without this, common
	// guard patterns like `if not ok then return end` would silently
	// stay unscored.
	src := []byte("if x then return end")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 1 {
		t.Fatalf("return-only body should get 1 injection, got %d", len(injections))
	}
}

func TestRewritePreservesLineCount(t *testing.T) {
	// Line-number preservation is a hard constraint from the design doc:
	// injections may not introduce newlines. Line coverage and stack
	// traces both key on line numbers and would silently misreport if
	// this ever regressed.
	src := []byte(`
if x then
  A()
elseif y then
  B()
else
  C()
end
`)
	out, _, _ := Rewrite("t.lua", src, RewriteOptions{})
	if strings.Count(string(out), "\n") != strings.Count(string(src), "\n") {
		t.Errorf("line count changed: original %d newlines, rewritten %d\n%s",
			strings.Count(string(src), "\n"), strings.Count(string(out), "\n"), out)
	}
}

func TestRewrittenSourceParses(t *testing.T) {
	// Round-trip: rewritten source must still be valid Lua. If injection
	// text produces invalid syntax, tests will fail with parse errors
	// that don't obviously trace back to this rewriter, so we lock it
	// in explicitly.
	src := []byte(`
local function f(x)
    if x > 0 then
        return x * 2
    elseif x < 0 then
        return -x
    else
        return 0
    end
end

while true do
    for i = 1, 10 do
        f(i)
    end
end
`)
	out, injections, err := Rewrite("t.lua", src, RewriteOptions{})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if len(injections) == 0 {
		t.Fatal("expected injections for a file with control flow")
	}
	if _, err := lua.Parse("t.lua", out); err != nil {
		t.Fatalf("rewritten source failed to parse:\n%s\nerror: %v", out, err)
	}
}

func TestRewriteNextIDUniqueAcrossFiles(t *testing.T) {
	// Multi-file runs share one NextID closure so branch IDs stay
	// unique across the whole run. Otherwise two files' injections
	// would collide and post-run attribution would mix them up.
	counter := 0
	nextID := func() int { counter++; return counter }

	_, inj1, _ := Rewrite("a.lua", []byte("if x then A() end"), RewriteOptions{NextID: nextID})
	_, inj2, _ := Rewrite("b.lua", []byte("if y then B() end"), RewriteOptions{NextID: nextID})

	if inj1[0].BranchID == inj2[0].BranchID {
		t.Errorf("IDs collided across files: a=%d b=%d", inj1[0].BranchID, inj2[0].BranchID)
	}
	if inj1[0].File != "a.lua" || inj2[0].File != "b.lua" {
		t.Errorf("File not carried through: a=%q b=%q", inj1[0].File, inj2[0].File)
	}
}

func TestRewriteCustomGlobalName(t *testing.T) {
	src := []byte("if x then A() end")
	out, _, _ := Rewrite("t.lua", src, RewriteOptions{GlobalName: "__my_counter"})
	if !strings.Contains(string(out), "__my_counter(1);") {
		t.Errorf("custom global not used:\n%s", out)
	}
}

func TestRewriteNestedControlFlow(t *testing.T) {
	// Nested control flow must produce distinct IDs in AST-walk order.
	// This is the same walk order the reporter uses, so IDs align with
	// BranchCoverage ordering without extra sorting.
	src := []byte(`for i = 1, 10 do
  if i > 5 then
    while i < 20 do i = i + 1 end
  end
end`)
	_, injections, err := Rewrite("t.lua", src, RewriteOptions{})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	// for-body + if-then + while-body = 3 injections
	if len(injections) != 3 {
		t.Errorf("got %d injections, want 3", len(injections))
	}
	seen := map[int]bool{}
	for _, inj := range injections {
		if seen[inj.BranchID] {
			t.Errorf("duplicate BranchID %d", inj.BranchID)
		}
		seen[inj.BranchID] = true
	}
}

func TestRewritePositionMetadata(t *testing.T) {
	// The Line/Column stored on an Injection must match the branch
	// decision's position (the `if` keyword, not the arm body). This
	// is what lets attribution reconstruct the domain BranchCoverage
	// entry from the injection alone.
	src := []byte("\n  if x then A() end")
	_, injections, _ := Rewrite("t.lua", src, RewriteOptions{})
	if len(injections) != 1 {
		t.Fatalf("got %d injections, want 1", len(injections))
	}
	if injections[0].Line != 2 || injections[0].Column != 3 {
		t.Errorf("Position = %d:%d, want 2:3", injections[0].Line, injections[0].Column)
	}
}

func TestRewriteNoInjectionsReturnsOriginalSrc(t *testing.T) {
	// A file with no control flow should return the original bytes,
	// not a re-serialized copy. This keeps the common case cheap and
	// ensures byte-identical output for files that need no rewriting.
	src := []byte("local x = 1\nreturn x")
	out, injections, err := Rewrite("t.lua", src, RewriteOptions{})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if len(injections) != 0 {
		t.Errorf("straight-line file should have no injections, got %d", len(injections))
	}
	if string(out) != string(src) {
		t.Errorf("output should equal input for no-injection case")
	}
}

func TestRewriteCorpusNeospecLua(t *testing.T) {
	// Acceptance test: neospec's own embedded Lua harness must rewrite
	// cleanly. If this ever fails, the runtime side of instrumentation
	// would ship broken source into the sandboxed Neovim.
	dir := "../runner/lua"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("corpus dir not readable: %v", err)
	}
	tested := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".lua" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			out, _, err := Rewrite(path, src, RewriteOptions{})
			if err != nil {
				t.Fatalf("rewrite parse error: %v", err)
			}
			// Line count must be preserved — see the design doc's
			// constraint 2 (line-number preservation).
			origNL := strings.Count(string(src), "\n")
			outNL := strings.Count(string(out), "\n")
			if origNL != outNL {
				t.Errorf("line count changed: original %d, rewritten %d", origNL, outNL)
			}
			// Rewritten source must still parse — round-trip check.
			if _, perr := lua.Parse(path, out); perr != nil {
				t.Errorf("rewritten source failed to parse: %v", perr)
			}
		})
		tested++
	}
	if tested == 0 {
		t.Fatal("no Lua files found in corpus dir")
	}
}
