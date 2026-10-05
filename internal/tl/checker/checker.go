package checker

import (
	"tl/internal/tl/ast"
	"tl/internal/tl/tlerr"
	"tl/internal/tl/token"
)

type FuncInfo struct {
	Name      string
	Params    []ast.Type
	Result    ast.Type
	Index     int
	NumLocals int
	Builtin   bool
	// Boxed marks local slots whose variables are captured by a closure;
	// they are stored as heap cells so mutations are shared and the
	// storage outlives the creating frame.
	Boxed map[int]bool
}

type binding struct {
	slot     int
	ty       ast.Type
	captured bool
}

type scope struct {
	parent *scope
	vars   map[string]*binding
}

// captureEnt is one captured variable of the current function context.
type captureEnt struct {
	name      string
	fromLocal bool // cell is a local slot of the parent function
	index     int  // parent local slot or parent capture index
	ty        ast.Type
}

// funcCtx tracks one function body (top-level declaration or lambda)
// while it is being checked.
type funcCtx struct {
	parent    *funcCtx
	name      string
	result    ast.Type
	scopes    *scope
	nextSlot  int
	captures  []captureEnt
	capByName map[string]int
	all       []*binding // every binding declared in this function
}

type Checker struct {
	file    string
	funcs   map[string]*FuncInfo
	order   []string
	ctx     *funcCtx
	curFunc *FuncInfo
	lambdas []*ast.FuncExpr
}

type Result struct {
	Funcs    map[string]*FuncInfo
	Order    []string
	MainName string
	// Lambdas lists every anonymous function in discovery order; each
	// node's Index field indexes this slice and the IR function table.
	Lambdas []*ast.FuncExpr
}

func Check(file string, prog *ast.Program) (*Result, error) {
	c := &Checker{file: file, funcs: map[string]*FuncInfo{}}
	// Built-in functions are available in every program.
	c.declareBuiltin("print", ast.TInt, ast.TInt)
	c.declareBuiltin("print_bool", ast.TBool, ast.TInt)

	for i, fn := range prog.Funcs {
		if _, exists := c.funcs[fn.Name]; exists {
			return nil, c.at(fn.Posn, "function %q redeclared", fn.Name)
		}
		params := make([]ast.Type, len(fn.Params))
		for j, p := range fn.Params {
			params[j] = p.Type
		}
		info := &FuncInfo{Name: fn.Name, Params: params, Result: fn.Result, Index: i}
		c.funcs[fn.Name] = info
		c.order = append(c.order, fn.Name)
	}
	if _, ok := c.funcs["main"]; !ok {
		return nil, c.at(token.Pos{Line: 1, Column: 1}, "program must declare function main() -> int")
	}
	mainInfo := c.funcs["main"]
	if len(mainInfo.Params) != 0 || mainInfo.Result != ast.TInt {
		return nil, c.at(prog.Funcs[mainInfo.Index].Posn, "main must have signature main() -> int")
	}

	for _, fn := range prog.Funcs {
		if err := c.checkFunc(fn); err != nil {
			return nil, err
		}
	}
	return &Result{Funcs: c.funcs, Order: c.order, MainName: "main", Lambdas: c.lambdas}, nil
}

func (c *Checker) declareBuiltin(name string, arg ast.Type, result ast.Type) {
	c.funcs[name] = &FuncInfo{Name: name, Params: []ast.Type{arg}, Result: result, Builtin: true, Index: -1}
}

func (c *Checker) at(pos token.Pos, format string, args ...any) error {
	return tlerr.At(c.file, pos, format, args...)
}

func (c *Checker) pushScope() {
	c.ctx.scopes = &scope{parent: c.ctx.scopes, vars: map[string]*binding{}}
}

func (c *Checker) popScope() {
	c.ctx.scopes = c.ctx.scopes.parent
}

func (c *Checker) declare(name string, ty ast.Type, pos token.Pos) (int, error) {
	if _, exists := c.ctx.scopes.vars[name]; exists {
		return 0, c.at(pos, "variable %q redeclared in this block", name)
	}
	slot := c.ctx.nextSlot
	c.ctx.nextSlot++
	b := &binding{slot: slot, ty: ty}
	c.ctx.scopes.vars[name] = b
	c.ctx.all = append(c.ctx.all, b)
	return slot, nil
}

// resolved describes how the current function reaches a variable.
type resolved struct {
	slot   int // local slot, or capture index when viaCap is set
	ty     ast.Type
	viaCap bool
}

func findLocal(ctx *funcCtx, name string) (*binding, bool) {
	for s := ctx.scopes; s != nil; s = s.parent {
		if b, ok := s.vars[name]; ok {
			return b, true
		}
	}
	return nil, false
}

// lookup resolves name from the current function, threading captures
// through any enclosing lambdas so closures share cells lexically.
func (c *Checker) lookup(name string, pos token.Pos) (resolved, error) {
	if b, ok := findLocal(c.ctx, name); ok {
		return resolved{slot: b.slot, ty: b.ty}, nil
	}
	if idx, ok := c.capture(c.ctx, name); ok {
		return resolved{slot: idx, ty: c.ctx.captures[idx].ty, viaCap: true}, nil
	}
	return resolved{}, c.at(pos, "undefined variable %q", name)
}

// capture ensures ctx has a capture entry for name, resolving it
// recursively through enclosing function contexts. It returns false when
// no enclosing function defines the name.
func (c *Checker) capture(ctx *funcCtx, name string) (int, bool) {
	if idx, ok := ctx.capByName[name]; ok {
		return idx, true
	}
	parent := ctx.parent
	if parent == nil {
		return 0, false
	}
	if b, ok := findLocal(parent, name); ok {
		b.captured = true
		return addCapture(ctx, name, true, b.slot, b.ty), true
	}
	pi, ok := c.capture(parent, name)
	if !ok {
		return 0, false
	}
	return addCapture(ctx, name, false, pi, parent.captures[pi].ty), true
}

func addCapture(ctx *funcCtx, name string, fromLocal bool, index int, ty ast.Type) int {
	if ctx.capByName == nil {
		ctx.capByName = map[string]int{}
	}
	ctx.captures = append(ctx.captures, captureEnt{name: name, fromLocal: fromLocal, index: index, ty: ty})
	ctx.capByName[name] = len(ctx.captures) - 1
	return len(ctx.captures) - 1
}

// boxedSlots snapshots which local slots of ctx were captured.
func boxedSlots(ctx *funcCtx) map[int]bool {
	boxed := map[int]bool{}
	for _, b := range ctx.all {
		if b.captured {
			boxed[b.slot] = true
		}
	}
	return boxed
}

func (c *Checker) checkFunc(fn *ast.FuncDecl) error {
	c.curFunc = c.funcs[fn.Name]
	c.ctx = &funcCtx{name: fn.Name, result: c.curFunc.Result}
	c.pushScope()
	seen := map[string]bool{}
	for _, p := range fn.Params {
		if seen[p.Name] {
			return c.at(p.Posn, "duplicate parameter %q", p.Name)
		}
		seen[p.Name] = true
		if _, err := c.declare(p.Name, p.Type, p.Posn); err != nil {
			return err
		}
	}
	if err := c.checkBlock(fn.Body, false); err != nil {
		return err
	}
	if !blockAlwaysReturns(fn.Body) {
		return c.at(fn.Body.Posn, "function %q must return a value on every path", fn.Name)
	}
	c.curFunc.NumLocals = c.ctx.nextSlot
	c.curFunc.Boxed = boxedSlots(c.ctx)
	c.ctx = nil
	return nil
}

func (c *Checker) checkBlock(b *ast.Block, newScope bool) error {
	if newScope {
		c.pushScope()
		defer c.popScope()
	}
	for _, s := range b.Stmts {
		if err := c.checkStmt(s); err != nil {
			return err
		}
	}
	return nil
}

func (c *Checker) checkStmt(s ast.Stmt) error {
	switch s := s.(type) {
	case *ast.LetStmt:
		if s.Init == nil {
			return c.at(s.Posn, "variable %q must be initialized", s.Name)
		}
		// A lambda bound by let may reference itself recursively, so its
		// variable is declared (and cell-allocated) before the body is
		// checked.
		if _, isFunc := s.Init.(*ast.FuncExpr); isFunc {
			slot, err := c.declare(s.Name, s.Type, s.Posn)
			if err != nil {
				return err
			}
			s.Slot = slot
			ty, err := c.checkExpr(s.Init)
			if err != nil {
				return err
			}
			if ty != s.Type {
				return c.at(s.Posn, "cannot initialize %s variable %q with %s value", s.Type, s.Name, ty)
			}
			return nil
		}
		ty, err := c.checkExpr(s.Init)
		if err != nil {
			return err
		}
		if ty != s.Type {
			return c.at(s.Posn, "cannot initialize %s variable %q with %s value", s.Type, s.Name, ty)
		}
		slot, err := c.declare(s.Name, s.Type, s.Posn)
		if err != nil {
			return err
		}
		s.Slot = slot
	case *ast.AssignStmt:
		b, err := c.lookup(s.Name, s.Posn)
		if err != nil {
			return err
		}
		ty, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if ty != b.ty {
			return c.at(s.Posn, "cannot assign %s value to %s variable %q", ty, b.ty, s.Name)
		}
		s.Slot = b.slot
		s.ViaCap = b.viaCap
	case *ast.ExprStmt:
		if _, err := c.checkExpr(s.X); err != nil {
			return err
		}
	case *ast.IfStmt:
		ty, err := c.checkExpr(s.Cond)
		if err != nil {
			return err
		}
		if ty != ast.TBool {
			return c.at(s.Cond.Pos(), "if condition must be bool, got %s", ty)
		}
		if err := c.checkBlock(s.Then, true); err != nil {
			return err
		}
		if s.Else != nil {
			if err := c.checkStmt(s.Else); err != nil {
				return err
			}
		}
	case *ast.WhileStmt:
		ty, err := c.checkExpr(s.Cond)
		if err != nil {
			return err
		}
		if ty != ast.TBool {
			return c.at(s.Cond.Pos(), "while condition must be bool, got %s", ty)
		}
		return c.checkBlock(s.Body, true)
	case *ast.ReturnStmt:
		ty, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if ty != c.ctx.result {
			return c.at(s.Posn, "cannot return %s from function %s(...) -> %s", ty, c.ctx.name, c.ctx.result)
		}
	case *ast.BlockStmt:
		return c.checkBlock(s.Block, true)
	default:
		return c.at(s.Pos(), "unsupported statement %T", s)
	}
	return nil
}

func (c *Checker) checkExpr(e ast.Expr) (ast.Type, error) {
	switch e := e.(type) {
	case *ast.IntLit:
		e.TType = ast.TInt
	case *ast.BoolLit:
		e.TType = ast.TBool
	case *ast.Ident:
		b, err := c.lookup(e.Name, e.Posn)
		if err != nil {
			// A top-level function name may be used as a value.
			fn, ok := c.funcs[e.Name]
			if !ok || fn.Builtin {
				return 0, err
			}
			params := make([]ast.Type, len(fn.Params))
			copy(params, fn.Params)
			e.IsFunc = true
			e.FuncIndex = fn.Index
			e.TType = ast.FuncOf(params, fn.Result)
			return e.TType, nil
		}
		e.Slot = b.slot
		e.TType = b.ty
		e.ViaCap = b.viaCap
	case *ast.UnaryExpr:
		t, err := c.checkExpr(e.X)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case token.MINUS:
			if t != ast.TInt {
				return 0, c.at(e.Posn, "unary '-' requires int, got %s", t)
			}
		case token.BANG:
			if t != ast.TBool {
				return 0, c.at(e.Posn, "unary '!' requires bool, got %s", t)
			}
		}
		e.TType = t
	case *ast.BinaryExpr:
		lt, err := c.checkExpr(e.L)
		if err != nil {
			return 0, err
		}
		rt, err := c.checkExpr(e.R)
		if err != nil {
			return 0, err
		}
		if lt != rt {
			return 0, c.at(e.Posn, "type mismatch in binary expression: %s vs %s", lt, rt)
		}
		if lt.IsFunc() {
			return 0, c.at(e.Posn, "operator %s is not defined for function values", e.Op)
		}
		switch e.Op {
		case token.EQ, token.NE:
			e.TType = ast.TBool
		case token.LT, token.GT, token.LE, token.GE:
			if lt != ast.TInt {
				return 0, c.at(e.Posn, "ordering comparison requires int operands")
			}
			e.TType = ast.TBool
		case token.AND, token.OR:
			if lt != ast.TBool {
				return 0, c.at(e.Posn, "logical operator requires bool operands")
			}
			e.TType = ast.TBool
		default:
			if lt != ast.TInt {
				return 0, c.at(e.Posn, "arithmetic operator requires int operands")
			}
			e.TType = ast.TInt
		}
	case *ast.CallExpr:
		// A variable in scope shadows the function namespace: f(x) calls
		// the stored closure when f is a local holding a function value.
		if r, err := c.lookup(e.Name, e.Posn); err == nil {
			if !r.ty.IsFunc() {
				return 0, c.at(e.Posn, "%q is a %s variable, not a function", e.Name, r.ty)
			}
			if err := c.checkCallArgs(e.Posn, e.Name, r.ty.Sig(), e.Args); err != nil {
				return 0, err
			}
			e.ValueCall = true
			e.Slot = r.slot
			e.ViaCap = r.viaCap
			e.Result = r.ty.Sig().Result
			return e.Result, nil
		}
		fn, ok := c.funcs[e.Name]
		if !ok {
			return 0, c.at(e.Posn, "undefined function %q", e.Name)
		}
		sig := &ast.FuncSig{Params: fn.Params, Result: fn.Result}
		if err := c.checkCallArgs(e.Posn, e.Name, sig, e.Args); err != nil {
			return 0, err
		}
		e.Builtin = fn.Builtin
		e.FuncIndex = fn.Index
		e.Result = fn.Result
	case *ast.CallValueExpr:
		ct, err := c.checkExpr(e.Callee)
		if err != nil {
			return 0, err
		}
		if !ct.IsFunc() {
			return 0, c.at(e.Callee.Pos(), "cannot call a %s value", ct)
		}
		if err := c.checkCallArgs(e.Posn, ct.String(), ct.Sig(), e.Args); err != nil {
			return 0, err
		}
		e.Result = ct.Sig().Result
	case *ast.FuncExpr:
		if err := c.checkFuncExpr(e); err != nil {
			return 0, err
		}
	default:
		return 0, c.at(e.Pos(), "unsupported expression %T", e)
	}
	return exprType(e), nil
}

func (c *Checker) checkCallArgs(pos token.Pos, name string, sig *ast.FuncSig, args []ast.Expr) error {
	if len(args) != len(sig.Params) {
		return c.at(pos, "function %q expects %d arguments, got %d", name, len(sig.Params), len(args))
	}
	for i, arg := range args {
		ty, err := c.checkExpr(arg)
		if err != nil {
			return err
		}
		if ty != sig.Params[i] {
			return c.at(arg.Pos(), "argument %d of %q expects %s, got %s", i+1, name, sig.Params[i], ty)
		}
	}
	return nil
}

// checkFuncExpr checks a lambda body in its own function context and
// records how it captures enclosing variables.
func (c *Checker) checkFuncExpr(e *ast.FuncExpr) error {
	parent := c.ctx
	ctx := &funcCtx{parent: parent, name: "anonymous", result: e.Result}
	c.ctx = ctx
	c.pushScope()
	seen := map[string]bool{}
	params := make([]ast.Type, len(e.Params))
	for i, p := range e.Params {
		if seen[p.Name] {
			return c.at(p.Posn, "duplicate parameter %q", p.Name)
		}
		seen[p.Name] = true
		params[i] = p.Type
		if _, err := c.declare(p.Name, p.Type, p.Posn); err != nil {
			return err
		}
	}
	if err := c.checkBlock(e.Body, false); err != nil {
		return err
	}
	if !blockAlwaysReturns(e.Body) {
		return c.at(e.Body.Posn, "anonymous function must return a value on every path")
	}
	e.Index = len(c.lambdas)
	e.NumLocals = ctx.nextSlot
	e.Boxed = boxedSlots(ctx)
	for _, cap := range ctx.captures {
		e.Caps = append(e.Caps, ast.Capture{FromLocal: cap.fromLocal, Index: cap.index})
	}
	c.lambdas = append(c.lambdas, e)
	c.ctx = parent
	e.TType = ast.FuncOf(params, e.Result)
	return nil
}

func exprType(e ast.Expr) ast.Type {
	switch e := e.(type) {
	case *ast.IntLit:
		return e.TType
	case *ast.BoolLit:
		return e.TType
	case *ast.Ident:
		return e.TType
	case *ast.UnaryExpr:
		return e.TType
	case *ast.BinaryExpr:
		return e.TType
	case *ast.CallExpr:
		return e.Result
	case *ast.CallValueExpr:
		return e.Result
	case *ast.FuncExpr:
		return e.TType
	default:
		return -1
	}
}

// blockAlwaysReturns implements a conservative definite-return analysis:
// a block returns if its final statement returns, so all preceding paths
// must already have terminated.
func blockAlwaysReturns(b *ast.Block) bool {
	if len(b.Stmts) == 0 {
		return false
	}
	return stmtAlwaysReturns(b.Stmts[len(b.Stmts)-1])
}

func stmtAlwaysReturns(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		return blockAlwaysReturns(s.Block)
	case *ast.IfStmt:
		if s.Else == nil {
			return false
		}
		if !blockAlwaysReturns(s.Then) {
			return false
		}
		return stmtAlwaysReturns(s.Else)
	}
	return false
}
