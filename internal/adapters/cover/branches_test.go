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
	src := "if x then y = 1 elseif z then y = 2 else y = 3 end"
	branches, err := FindBranches("t.lua", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := kinds(branches)
	want := []string{"if", "elseif"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("kinds = %v, want %v", got, want)
	}
}

func TestFindBranchesWhile(t *testing.T) {
	branches, _ := FindBranches("t.lua", []byte("while x do y = 1 end"))
	if len(branches) != 1 || branches[0].Kind != "while" {
		t.Errorf("got %v, want [while]", kinds(branches))
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

func TestFindBranchesAndOr(t *testing.T) {
	// `a and b or c` builds as ((a and b) or c) — `or` has lower precedence,
	// so it is the outer BinaryExpr; the visitor sees `or` first, then `and`.
	branches, _ := FindBranches("t.lua", []byte("local x = a and b or c"))
	got := kinds(branches)
	want := []string{"or", "and"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("kinds = %v, want %v", got, want)
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
	// Walk order: outermost first — for, if, while.
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

func TestFindBranchesUnparseableReturnsNilAndError(t *testing.T) {
	// The current parser returns a non-nil chunk even on lex errors, so
	// this test uses input that would only fail if the parser gave up
	// entirely. Today FindBranches gets a chunk + error and walks it —
	// verify that error is propagated so callers know to distrust results.
	branches, err := FindBranches("t.lua", []byte(`"unterminated`))
	if err == nil {
		t.Fatal("expected error from unterminated string")
	}
	// Branches may be empty or contain nothing — the contract is only that
	// the error surfaces.
	_ = branches
}

func TestFindBranchesPartialAfterRecovery(t *testing.T) {
	// A bad first statement should not prevent the parser from recovering
	// and the walker from reaching the if that follows.
	src := "xx yy\nif x then end"
	branches, err := FindBranches("t.lua", []byte(src))
	if err == nil {
		t.Fatal("expected parse error")
	}
	if len(branches) != 1 || branches[0].Kind != "if" {
		t.Errorf("got %v; expected recovery to yield [if]", kinds(branches))
	}
}
