package cover

import (
	"fmt"
	"strings"

	lua "github.com/jedi-knights/go-lua-parser"
)

// FunctionInfo names one function definition located in Lua source. Line
// is the 1-based line of the `function` keyword — matches
// debug.getinfo(...).linedefined, so the runtime hook can look this up
// by line without further translation. Name is the recovered name,
// falling back to "anonymous@N" only for expressions that genuinely
// have no local name (e.g., a function value passed inline to a call).
//
// This replaces coverage_hook.lua's NAME_PATTERNS pattern-matching: the
// AST already carries the shape information the six patterns tried to
// approximate. Names are correct for every form the patterns handle
// plus multi-line signatures, table-literal method fields, and other
// shapes the patterns fall through to "anonymous@N".
type FunctionInfo struct {
	Line int
	Name string
}

// ExtractFunctions parses src and returns one FunctionInfo per function
// definition. On parse errors the partial AST is still walked so a file
// with recoverable errors produces best-effort output; the error is
// returned so the caller can decide whether to trust it.
//
// Order is source-order (walk order): the first function defined in the
// file appears first. Callers that key by line for O(1) lookup should
// pick one policy on collision (two functions on the same line) — the
// last one wins in a plain map assignment, which mirrors the existing
// runtime's line-keyed storage.
func ExtractFunctions(filename string, src []byte) ([]FunctionInfo, error) {
	chunk, err := lua.Parse(filename, src)
	if chunk == nil {
		return nil, err
	}
	e := &funcExtractor{}
	e.walkBlock(chunk.Block)
	return e.funcs, err
}

// funcExtractor walks the AST with explicit context propagation — it
// does not use lua.Walk because Visit does not tell you which slot of
// the parent you are in, and the whole point is to thread the "name
// hint from the enclosing binding" down to each FunctionExpr.
type funcExtractor struct {
	funcs []FunctionInfo
}

func (e *funcExtractor) walkBlock(block *lua.Block) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		e.walkStatement(stmt)
	}
	if block.Return != nil {
		for _, v := range block.Return.Values {
			e.walkExpression(v, "")
		}
	}
}

func (e *funcExtractor) walkStatement(stmt lua.Statement) {
	switch s := stmt.(type) {
	case *lua.FuncDeclStat:
		e.recordFunc(s.Body, formatFuncName(s.Name))
	case *lua.LocalFuncStat:
		e.recordFunc(s.Body, s.Name.Name)
	case *lua.LocalAssignStat:
		e.walkNamedValues(namesToHints(s.Names), s.Values)
	case *lua.AssignStat:
		e.walkNamedValues(targetsToHints(s.Targets), s.Values)
	case *lua.CallStat:
		e.walkExpression(s.Call, "")
	case *lua.DoStat:
		e.walkBlock(s.Body)
	case *lua.WhileStat:
		e.walkExpression(s.Cond, "")
		e.walkBlock(s.Body)
	case *lua.RepeatStat:
		e.walkBlock(s.Body)
		e.walkExpression(s.Cond, "")
	case *lua.IfStat:
		e.walkIf(s)
	case *lua.NumericForStat:
		e.walkExpression(s.Start, "")
		e.walkExpression(s.Stop, "")
		if s.Step != nil {
			e.walkExpression(s.Step, "")
		}
		e.walkBlock(s.Body)
	case *lua.GenericForStat:
		for _, v := range s.Values {
			e.walkExpression(v, "")
		}
		e.walkBlock(s.Body)
	}
}

// walkIf is split out of walkStatement so the type switch stays under
// the cyclomatic-complexity budget.
func (e *funcExtractor) walkIf(s *lua.IfStat) {
	e.walkExpression(s.Cond, "")
	e.walkBlock(s.Then)
	for _, ei := range s.ElseIfs {
		e.walkExpression(ei.Cond, "")
		e.walkBlock(ei.Body)
	}
	if s.Else != nil {
		e.walkBlock(s.Else)
	}
}

// walkNamedValues pairs each value with its corresponding name hint by
// slot index. `local f, g = function() end, function() end` gives both
// functions their real names via this pairing; anything longer than the
// names list falls back to "" and is labelled anonymous.
func (e *funcExtractor) walkNamedValues(hints []string, values []lua.Expression) {
	for i, v := range values {
		hint := ""
		if i < len(hints) {
			hint = hints[i]
		}
		e.walkExpression(v, hint)
	}
}

// walkExpression descends into an expression, propagating the name hint
// only to a top-level FunctionExpr. Nested expressions inside the
// function's arguments etc. do not inherit the outer hint — they get
// their own labels.
func (e *funcExtractor) walkExpression(expr lua.Expression, nameHint string) {
	if expr == nil {
		return
	}
	switch x := expr.(type) {
	case *lua.FunctionExpr:
		e.recordFuncExpr(x, nameHint)
	case *lua.BinaryExpr:
		e.walkExpression(x.Left, "")
		e.walkExpression(x.Right, "")
	case *lua.UnaryExpr:
		e.walkExpression(x.Operand, "")
	case *lua.CallExpr:
		e.walkExpression(x.Fn, "")
		for _, a := range x.Args {
			e.walkExpression(a, "")
		}
	case *lua.MethodCallExpr:
		e.walkExpression(x.Object, "")
		for _, a := range x.Args {
			e.walkExpression(a, "")
		}
	case *lua.IndexExpr:
		e.walkExpression(x.Object, "")
		e.walkExpression(x.Index, "")
	case *lua.FieldExpr:
		e.walkExpression(x.Object, "")
	case *lua.TableExpr:
		e.walkTable(x)
	}
}

// walkTable propagates the field's key name as the hint for a function
// value — `M = { foo = function() end }` should record the function as
// `foo`, not `anonymous@N`. Keys that are not name-shaped (positional
// entries, `[expr] = ...`) do not propagate a hint.
func (e *funcExtractor) walkTable(t *lua.TableExpr) {
	for _, f := range t.Fields {
		hint := f.KeyName
		if f.Key != nil {
			e.walkExpression(f.Key, "")
		}
		if f.Value != nil {
			e.walkExpression(f.Value, hint)
		}
	}
}

// recordFunc emits a FunctionInfo for a named FunctionExpr, then
// descends into the body so nested functions are also recorded.
func (e *funcExtractor) recordFunc(fn *lua.FunctionExpr, name string) {
	if fn == nil {
		return
	}
	e.funcs = append(e.funcs, FunctionInfo{Line: fn.Position.Line, Name: name})
	e.walkBlock(fn.Body)
}

// recordFuncExpr emits a FunctionInfo for a bare function expression,
// using the caller's name hint or falling back to a positional label.
func (e *funcExtractor) recordFuncExpr(fn *lua.FunctionExpr, nameHint string) {
	name := nameHint
	if name == "" {
		name = fmt.Sprintf("anonymous@%d", fn.Position.Line)
	}
	e.recordFunc(fn, name)
}

// formatFuncName renders a FuncName as `root.dot.chain:method`, the
// same shape the existing NAME_PATTERNS produced for dotted / method
// declarations. Preserves the runtime consumer's expected label format
// so switching the source of the string doesn't ripple into reporter
// output.
func formatFuncName(fn *lua.FuncName) string {
	if fn == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fn.Root.Name)
	for _, d := range fn.Dots {
		b.WriteByte('.')
		b.WriteString(d.Name)
	}
	if fn.Method != nil {
		b.WriteByte(':')
		b.WriteString(fn.Method.Name)
	}
	return b.String()
}

// namesToHints extracts the plain name from each *lua.Ident in a
// LocalAssignStat's Names slice.
func namesToHints(names []*lua.Ident) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n.Name
	}
	return out
}

// targetsToHints renders each assignment target as a source-like name.
// Ident and FieldExpr chains produce useful hints; IndexExpr targets
// like `t[1] = function` produce no hint (empty string), so the
// resulting function is labelled anonymous.
func targetsToHints(targets []lua.Expression) []string {
	out := make([]string, len(targets))
	for i, t := range targets {
		out[i] = renderTarget(t)
	}
	return out
}

// renderTarget renders an assignable expression as a source-like
// name string. Recurses on FieldExpr; returns "" for shapes without a
// meaningful text name.
func renderTarget(expr lua.Expression) string {
	switch x := expr.(type) {
	case *lua.Ident:
		return x.Name
	case *lua.FieldExpr:
		inner := renderTarget(x.Object)
		if inner == "" {
			return x.Name
		}
		return inner + "." + x.Name
	}
	return ""
}
