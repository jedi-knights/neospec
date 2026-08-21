package cover

import (
	"bytes"
	"fmt"
	"sort"

	lua "github.com/jedi-knights/go-lua-parser"
)

// Injection is a single splice point produced by Rewrite. The rewriter
// hands the caller both the rewritten source and the list of injections
// so that runtime counter values collected under the injected global
// can be attributed back to a BranchCoverage arm.
//
// See docs/branch-instrumentation-design.md for the enclosing design.
type Injection struct {
	// File is the filename passed to Rewrite. Held for downstream ID
	// mapping so a single []Injection can cover a multi-file run.
	File string
	// Offset is the byte position in the original source where Text is
	// spliced in.
	Offset int
	// Text is the exact bytes to insert (typically "_neospec_br(N); ").
	Text string
	// BranchID is the argument passed to the runtime counter global.
	// IDs are unique within a Rewrite call; callers that rewrite
	// multiple files pass a shared RewriteOptions.NextID to keep IDs
	// unique across files.
	BranchID int
	// Line and Column identify the branch decision (matching the
	// domain BranchCoverage.Line/Column). ArmIndex identifies which
	// arm of that branch this injection scores.
	Line     int
	Column   int
	ArmIndex int
}

// RewriteOptions configures the rewriter.
type RewriteOptions struct {
	// GlobalName is the Lua global called with the branch ID at each
	// injection point. Defaults to "_neospec_br".
	GlobalName string
	// NextID supplies a unique branch ID for each injection. Callers
	// rewriting multiple files should pass the same closure so IDs
	// stay unique globally. If nil, Rewrite uses a per-call counter
	// starting at 1.
	NextID func() int
}

// Rewrite parses src and plans a Phase 1 injection at every statement-
// position arm body (if/elseif/else/while/repeat/for). The returned
// source has the injections spliced in; the returned []Injection lists
// each splice with the metadata needed to attribute runtime counter
// values back to BranchCoverage arms.
//
// Phase 1 scope (see docs/branch-instrumentation-design.md): same-line
// resolution only. Short-circuit and/or (Phase 2) and implicit-arm
// synthesis (Phase 3) are intentionally out of scope — the rewriter
// touches only explicit statement-position arms whose body has at
// least one statement or a return.
//
// Line count is preserved: no injection introduces a newline, so
// debug.getinfo(...).currentline and the existing line-coverage hook
// see the same numbers as they would for the original source.
// Callers should still load the result with load(src, "@"..original,
// "t") so debug.getinfo(...).source resolves to the user's file.
//
// On parse error the partial AST is still walked so files with
// recoverable errors produce a best-effort rewrite. The error is
// returned so callers can decide whether to trust the output.
func Rewrite(filename string, src []byte, opts RewriteOptions) ([]byte, []Injection, error) {
	if opts.GlobalName == "" {
		opts.GlobalName = "_neospec_br"
	}
	if opts.NextID == nil {
		counter := 0
		opts.NextID = func() int { counter++; return counter }
	}

	chunk, err := lua.Parse(filename, src)
	if chunk == nil {
		return src, nil, err
	}

	planner := &rewritePlanner{opts: opts, file: filename}
	lua.Walk(planner, chunk)

	if len(planner.injections) == 0 {
		return src, nil, err
	}
	return splice(src, planner.injections), planner.injections, err
}

// splice returns a new byte slice with each Injection.Text inserted at
// its Offset. Walks the source once in ascending offset order, so
// output is built linearly with no intermediate copies per injection.
func splice(src []byte, injections []Injection) []byte {
	sorted := make([]Injection, len(injections))
	copy(sorted, injections)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Offset < sorted[j].Offset
	})

	var out bytes.Buffer
	out.Grow(len(src) + totalTextLength(sorted))
	last := 0
	for _, inj := range sorted {
		out.Write(src[last:inj.Offset])
		out.WriteString(inj.Text)
		last = inj.Offset
	}
	out.Write(src[last:])
	return out.Bytes()
}

func totalTextLength(injections []Injection) int {
	n := 0
	for _, inj := range injections {
		n += len(inj.Text)
	}
	return n
}

// rewritePlanner walks the AST and appends an Injection for every
// statement-position arm body. Empty bodies are skipped — nothing to
// count.
type rewritePlanner struct {
	opts       RewriteOptions
	file       string
	injections []Injection
}

// Visit records the current node if it introduces a branch, then
// returns the same visitor to descend into children.
func (p *rewritePlanner) Visit(node lua.Node) lua.Visitor {
	if node == nil {
		return p
	}
	p.plan(node)
	return p
}

// plan dispatches to the per-kind planner.
func (p *rewritePlanner) plan(node lua.Node) {
	switch n := node.(type) {
	case *lua.IfStat:
		p.planIf(n)
	case *lua.WhileStat:
		p.planArm(n.Position, 0, n.Body)
	case *lua.RepeatStat:
		p.planArm(n.Position, 0, n.Body)
	case *lua.NumericForStat:
		p.planArm(n.Position, 0, n.Body)
	case *lua.GenericForStat:
		p.planArm(n.Position, 0, n.Body)
	}
}

// planIf schedules injections for every explicit arm body, mapping each
// to (BranchPosition, ArmIndex) so ApplyBranchCounters can find the
// right domain BranchCoverage arm.
//
// The detector (branches.go) creates ONE 2-arm branch per decision — a
// branch at the `if` position with arms [then, else-fallthrough], and
// a separate branch at each `elseif` position with arms [then, else-
// fallthrough]. So the injection at an elseif body must attribute to
// arm 0 of the elseif's OWN branch, not to arm 1+i of some virtual
// combined branch. An earlier version got this wrong (used 1+i) and
// silently overwrote the elseif's fall-through arm with the elseif-
// body's hit count — the runtime integration test that landed
// alongside this rewrite is what surfaced it.
//
// The else body is arm 1 of the LAST decision branch (the last elseif,
// or the if when no elseifs). PopulateBranches's line-hit derivation
// would already give the right count for a distinct-line else clause;
// the instrumentation counter overrides with the exact value, which
// helps only for same-line else constructs but never disagrees with
// the derived value on distinct-line ones.
//
// Implicit fall-through arms without any body (an `if` with no else)
// stay unhandled here — that is Phase 3 territory.
func (p *rewritePlanner) planIf(n *lua.IfStat) {
	p.planArm(n.Position, 0, n.Then)
	for _, ei := range n.ElseIfs {
		p.planArm(ei.Position, 0, ei.Body)
	}
	if n.Else != nil {
		branchPos := n.Position
		if len(n.ElseIfs) > 0 {
			branchPos = n.ElseIfs[len(n.ElseIfs)-1].Position
		}
		p.planArm(branchPos, 1, n.Else)
	}
}

// planArm records an injection at the arm body's first statement
// offset, or the return statement's offset when Statements is empty.
// Empty bodies (neither statements nor a return) are skipped — there
// is nothing observable to count.
func (p *rewritePlanner) planArm(branchPos lua.Position, armIdx int, body *lua.Block) {
	off, ok := armBodyOffset(body)
	if !ok {
		return
	}
	id := p.opts.NextID()
	p.injections = append(p.injections, Injection{
		File:     p.file,
		Offset:   off,
		Text:     fmt.Sprintf("%s(%d); ", p.opts.GlobalName, id),
		BranchID: id,
		Line:     branchPos.Line,
		Column:   branchPos.Column,
		ArmIndex: armIdx,
	})
}

// armBodyOffset returns the byte offset where an injection should be
// spliced in for a block's first observable statement, or (0, false)
// when the block is nil or empty.
func armBodyOffset(block *lua.Block) (int, bool) {
	if block == nil {
		return 0, false
	}
	if len(block.Statements) > 0 {
		return block.Statements[0].Pos().Offset, true
	}
	if block.Return != nil {
		return block.Return.Position.Offset, true
	}
	return 0, false
}
