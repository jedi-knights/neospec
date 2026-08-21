package cover

import "testing"

func kinds(bs []Branch) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Kind
	}
	return out
}

func TestFindBranchesIfElseif(t *testing.T) {
	// `if x then A elseif z then B else C end` — one branch per decision:
	// the initial if and one elseif. Each branch carries two arms whose
	// FirstLine points at the body's first statement so consumers can
	// score "taken" by looking up the line in coverage data.
	src := "if x then\n  y = 1\nelseif z then\n  y = 2\nelse\n  y = 3\nend"
	branches, err := FindBranches("t.lua", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := kinds(branches)
	want := []string{"if", "elseif"}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	// The initial `if` falls through to the elseif's body when false.
	if branches[0].Arms[0].FirstLine != 2 {
		t.Errorf("if then-arm FirstLine = %d, want 2", branches[0].Arms[0].FirstLine)
	}
	if branches[0].Arms[1].FirstLine != 4 {
		t.Errorf("if else-arm FirstLine = %d (should point at elseif body), want 4", branches[0].Arms[1].FirstLine)
	}
	// The elseif falls through to the else block.
	if branches[1].Arms[1].FirstLine != 6 {
		t.Errorf("elseif else-arm FirstLine = %d, want 6", branches[1].Arms[1].FirstLine)
	}
}

func TestFindBranchesIfNoElse(t *testing.T) {
	// Without an else, the fall-through arm has no locatable body — its
	// FirstLine is 0 so downstream code renders it as "unknown".
	branches, _ := FindBranches("t.lua", []byte("if x then\n  y = 1\nend"))
	if len(branches) != 1 {
		t.Fatalf("got %d branches, want 1", len(branches))
	}
	if branches[0].Arms[0].FirstLine != 2 {
		t.Errorf("then-arm FirstLine = %d, want 2", branches[0].Arms[0].FirstLine)
	}
	if branches[0].Arms[1].FirstLine != 0 {
		t.Errorf("else-arm FirstLine = %d, want 0 (unknown)", branches[0].Arms[1].FirstLine)
	}
}

func TestFindBranchesWhile(t *testing.T) {
	branches, _ := FindBranches("t.lua", []byte("while x do\n  y = 1\nend"))
	if len(branches) != 1 || branches[0].Kind != "while" {
		t.Fatalf("got %v, want [while]", kinds(branches))
	}
	if branches[0].Arms[0].FirstLine != 2 || branches[0].Arms[0].Label != "body" {
		t.Errorf("body arm = %+v", branches[0].Arms[0])
	}
	if branches[0].Arms[1].FirstLine != 0 || branches[0].Arms[1].Label != "exit" {
		t.Errorf("exit arm = %+v", branches[0].Arms[1])
	}
}

func TestFindBranchesRepeat(t *testing.T) {
	branches, _ := FindBranches("t.lua", []byte("repeat x = 1 until x"))
	if len(branches) != 1 || branches[0].Kind != "repeat" {
		t.Errorf("got %v, want [repeat]", kinds(branches))
	}
}

func TestFindBranchesNumericFor(t *testing.T) {
	branches, _ := FindBranches("t.lua", []byte("for i = 1, 10 do end"))
	if len(branches) != 1 || branches[0].Kind != "for" {
		t.Errorf("got %v, want [for]", kinds(branches))
	}
}

func TestFindBranchesGenericFor(t *testing.T) {
	branches, _ := FindBranches("t.lua", []byte("for k, v in pairs(t) do end"))
	if len(branches) != 1 || branches[0].Kind != "for" {
		t.Errorf("got %v, want [for]", kinds(branches))
	}
}

func TestFindBranchesShortCircuitNotEmitted(t *testing.T) {
	// `and` / `or` are deliberately not reported yet — their per-arm hit
	// counts cannot be derived from line coverage alone, so emitting them
	// would inflate BRF without a matching BRH signal. Locking this in so
	// a future addition is an explicit decision.
	branches, _ := FindBranches("t.lua", []byte("local x = a and b or c"))
	if len(branches) != 0 {
		t.Errorf("got %d branches, want 0 (short-circuit ops not reported)", len(branches))
	}
}

func TestFindBranchesNested(t *testing.T) {
	src := `
for i = 1, 10 do
  if i > 5 then
    while i < 20 do i = i + 1 end
  end
end`
	branches, err := FindBranches("t.lua", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := kinds(branches)
	want := []string{"for", "if", "while"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("branch %d: got %s, want %s", i, got[i], want[i])
		}
	}
}

func TestFindBranchesPosition(t *testing.T) {
	// Two leading blank lines + two-space indent → the if starts at line 3, col 3.
	src := "\n\n  if x then end"
	branches, err := FindBranches("t.lua", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(branches) != 1 {
		t.Fatalf("got %d branches, want 1", len(branches))
	}
	if branches[0].Line != 3 || branches[0].Column != 3 {
		t.Errorf("position = %d:%d, want 3:3", branches[0].Line, branches[0].Column)
	}
}

func TestFindBranchesEmpty(t *testing.T) {
	branches, err := FindBranches("t.lua", []byte("local x = 1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(branches) != 0 {
		t.Errorf("got %d branches, want 0", len(branches))
	}
}

func TestFindBranchesUnparseableReturnsError(t *testing.T) {
	branches, err := FindBranches("t.lua", []byte(`"unterminated`))
	if err == nil {
		t.Fatal("expected error from unterminated string")
	}
	_ = branches
}

func TestFindBranchesPartialAfterRecovery(t *testing.T) {
	src := "xx yy\nif x then end"
	branches, err := FindBranches("t.lua", []byte(src))
	if err == nil {
		t.Fatal("expected parse error")
	}
	if len(branches) != 1 || branches[0].Kind != "if" {
		t.Errorf("got %v; expected recovery to yield [if]", kinds(branches))
	}
}

func TestFindBranchesReturnAsFirstBodyStatement(t *testing.T) {
	// firstLine falls back to Block.Return when Statements is empty,
	// which is the shape of `if x then return end` — the return is the
	// entire body and the arm should still get a line.
	branches, _ := FindBranches("t.lua", []byte("if x then\n  return\nend"))
	if len(branches) != 1 {
		t.Fatalf("got %d branches, want 1", len(branches))
	}
	if branches[0].Arms[0].FirstLine != 2 {
		t.Errorf("then-arm FirstLine = %d, want 2 (points at return)", branches[0].Arms[0].FirstLine)
	}
}
