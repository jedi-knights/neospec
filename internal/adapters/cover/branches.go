package cover

import (
	lua "github.com/jedi-knights/go-lua-parser"
)

// Branch identifies one source-derived branch point in Lua: a control-flow
// site where execution can go one of at least two ways. Line and Column
// locate the deciding keyword; Arms lists the alternative paths so callers
// can attribute runtime hit counts to each arm.
type Branch struct {
	Line   int
	Column int
	Kind   string // one of: "if", "elseif", "while", "repeat", "for"
	Arms   []Arm
}

// Arm identifies one alternative code path from a branch point. FirstLine
// is the 1-based line number of the arm's first executable statement, or
// 0 if the arm has no locatable body (e.g., the implicit fall-through of
// an `if` without an `else`). Consumers that derive hit counts from line
// coverage treat a 0 FirstLine as "unknown".
type Arm struct {
	FirstLine int
	Label     string // "then" | "else" | "body"
}

// FindBranches parses src and returns every branch point in the resulting
// AST. On parse errors the partial AST is still walked so callers see
// whatever branches were recoverable; the parse error is returned
// alongside so the caller can decide whether to trust the result.
//
// Short-circuit operators (`and`, `or`) are not reported: their per-arm
// hit counts cannot be honestly derived from line-level coverage, so
// emitting them would inflate the branch total without a matching hit
// signal. Add them in a follow-up when instrumentation is available.
func FindBranches(filename string, src []byte) ([]Branch, error) {
	chunk, err := lua.Parse(filename, src)
	if chunk == nil {
		return nil, err
	}
	d := &branchDetector{}
	lua.Walk(d, chunk)
	return d.branches, err
}

// branchDetector implements lua.Visitor. It records one Branch per
// control-flow node encountered during the depth-first walk.
type branchDetector struct {
	branches []Branch
}

// Visit records the current node if it is a branch point, then returns
// the same visitor to descend into children.
func (d *branchDetector) Visit(node lua.Node) lua.Visitor {
	if node == nil {
		return d
	}
	d.record(node)
	return d
}

// record dispatches to the per-kind emitter based on the node type.
func (d *branchDetector) record(node lua.Node) {
	switch n := node.(type) {
	case *lua.IfStat:
		d.recordIf(n)
	case *lua.WhileStat:
		d.recordLoop(n.Position, "while", n.Body)
	case *lua.RepeatStat:
		d.recordLoop(n.Position, "repeat", n.Body)
	case *lua.NumericForStat:
		d.recordLoop(n.Position, "for", n.Body)
	case *lua.GenericForStat:
		d.recordLoop(n.Position, "for", n.Body)
	}
}

// recordIf emits one branch per decision (the initial `if` plus each
// `elseif`), each with two arms: "then" for the arm's body and "else"
// for the fall-through target. The fall-through's FirstLine is populated
// when we can point at concrete code (the next elseif's body or the else
// block); otherwise it is 0 and the reporter treats the count as unknown.
func (d *branchDetector) recordIf(n *lua.IfStat) {
	d.branches = append(d.branches, Branch{
		Line: n.Position.Line, Column: n.Position.Column, Kind: "if",
		Arms: []Arm{
			{FirstLine: firstLine(n.Then), Label: "then"},
			{FirstLine: fallthroughLine(n, -1), Label: "else"},
		},
	})
	for i, ei := range n.ElseIfs {
		d.branches = append(d.branches, Branch{
			Line: ei.Position.Line, Column: ei.Position.Column, Kind: "elseif",
			Arms: []Arm{
				{FirstLine: firstLine(ei.Body), Label: "then"},
				{FirstLine: fallthroughLine(n, i), Label: "else"},
			},
		})
	}
}

// recordLoop emits a two-arm branch for a loop: "body" for at-least-one
// iteration and "exit" for zero-iteration / loop-terminated. The exit
// arm has no locatable line without a following statement to inspect,
// so it is left as 0 (unknown to consumers).
func (d *branchDetector) recordLoop(pos lua.Position, kind string, body *lua.Block) {
	d.branches = append(d.branches, Branch{
		Line: pos.Line, Column: pos.Column, Kind: kind,
		Arms: []Arm{
			{FirstLine: firstLine(body), Label: "body"},
			{FirstLine: 0, Label: "exit"},
		},
	})
}

// firstLine returns the 1-based line number of block's first executable
// statement, or 0 if the block is nil or empty. Used to score the "taken"
// arm of a branch by looking up the returned line in coverage data.
func firstLine(block *lua.Block) int {
	if block == nil {
		return 0
	}
	if len(block.Statements) > 0 {
		return block.Statements[0].Pos().Line
	}
	if block.Return != nil {
		return block.Return.Position.Line
	}
	return 0
}

// fallthroughLine returns the first line of whatever an if-decision falls
// through to when its condition is false. afterIdx is the index of the
// elseif whose false-branch we are describing (-1 for the initial if).
// Returns 0 when the target is not locatable — the reporter treats that
// as "unknown taken count" rather than "zero taken".
func fallthroughLine(n *lua.IfStat, afterIdx int) int {
	if next := afterIdx + 1; next < len(n.ElseIfs) {
		return firstLine(n.ElseIfs[next].Body)
	}
	if n.Else != nil {
		return firstLine(n.Else)
	}
	return 0
}
