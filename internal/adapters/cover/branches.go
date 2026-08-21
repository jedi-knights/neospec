package cover

import (
	lua "github.com/jedi-knights/go-lua-parser"
)

// Branch identifies one source-derived branch point in Lua: a control-flow
// site where execution can go one of at least two ways. Line and Column
// locate the deciding keyword or operator.
type Branch struct {
	Line   int
	Column int
	Kind   string // one of: "if", "elseif", "while", "repeat", "for", "and", "or"
}

// FindBranches parses src and returns every branch point in the resulting
// AST. On parse errors the partial AST is still walked so callers see
// whatever branches were recoverable; the parse error is returned
// alongside so the caller can decide whether to trust the result.
//
// This is the input side of BRDA emission — matching it up with runtime
// hit counts happens in the reporter.
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

// record inspects one AST node and appends any branches it represents.
// Kept separate from Visit so future additions (short-circuit `and`/`or`
// in conditional positions, ternary-style `a and b or c`) can extend the
// switch without touching the traversal contract.
func (d *branchDetector) record(node lua.Node) {
	switch n := node.(type) {
	case *lua.IfStat:
		d.add(n.Position, "if")
		for _, ei := range n.ElseIfs {
			d.add(ei.Position, "elseif")
		}
	case *lua.WhileStat:
		d.add(n.Position, "while")
	case *lua.RepeatStat:
		d.add(n.Position, "repeat")
	case *lua.NumericForStat:
		d.add(n.Position, "for")
	case *lua.GenericForStat:
		d.add(n.Position, "for")
	case *lua.BinaryExpr:
		if n.Op == "and" || n.Op == "or" {
			d.add(n.Position, n.Op)
		}
	}
}

func (d *branchDetector) add(pos lua.Position, kind string) {
	d.branches = append(d.branches, Branch{
		Line:   pos.Line,
		Column: pos.Column,
		Kind:   kind,
	})
}
