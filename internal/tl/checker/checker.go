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
}

type binding struct {
	slot int
	ty   ast.Type
}

type scope struct {
	parent *scope
	vars   map[string]binding
}

type Checker struct {
	file     string
	funcs    map[string]*FuncInfo
	order    []string
	scopes   *scope
	curFunc  *FuncInfo
	nextSlot int
}

type Result struct {
	Funcs    map[string]*FuncInfo
	Order    []string
	MainName string
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
	return &Result{Funcs: c.funcs, Order: c.order, MainName: "main"}, nil
}

func (c *Checker) declareBuiltin(name string, arg ast.Type, result ast.Type) {
	c.funcs[name] = &FuncInfo{Name: name, Params: []ast.Type{arg}, Result: result, Builtin: true, Index: -1}
}

func (c *Checker) at(pos token.Pos, format string, args ...any) error {
	return tlerr.At(c.file, pos, format, args...)
}

func (c *Checker) pushScope() {
	c.scopes = &scope{parent: c.scopes, vars: map[string]binding{}}
}

func (c *Checker) popScope() {
	c.scopes = c.scopes.parent
}

func (c *Checker) declare(name string, ty ast.Type, pos token.Pos) (int, error) {
	if _, exists := c.scopes.vars[name]; exists {
		return 0, c.at(pos, "variable %q redeclared in this block", name)
	}
	slot := c.nextSlot
	c.nextSlot++
	c.scopes.vars[name] = binding{slot: slot, ty: ty}
	return slot, nil
}

func (c *Checker) lookup(name string, pos token.Pos) (binding, error) {
	for s := c.scopes; s != nil; s = s.parent {
		if b, ok := s.vars[name]; ok {
			return b, nil
		}
	}
	return binding{}, c.at(pos, "undefined variable %q", name)
}

func (c *Checker) checkFunc(fn *ast.FuncDecl) error {
	c.curFunc = c.funcs[fn.Name]
	c.scopes = nil
	c.nextSlot = 0
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
	c.curFunc.NumLocals = c.nextSlot
	c.scopes = nil
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
		if ty != c.curFunc.Result {
			return c.at(s.Posn, "cannot return %s from function %s(...) -> %s", ty, c.curFunc.Name, c.curFunc.Result)
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
			return 0, err
		}
		e.Slot = b.slot
		e.TType = b.ty
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
		fn, ok := c.funcs[e.Name]
		if !ok {
			return 0, c.at(e.Posn, "undefined function %q", e.Name)
		}
		if len(e.Args) != len(fn.Params) {
			return 0, c.at(e.Posn, "function %q expects %d arguments, got %d", e.Name, len(fn.Params), len(e.Args))
		}
		for i, arg := range e.Args {
			ty, err := c.checkExpr(arg)
			if err != nil {
				return 0, err
			}
			if ty != fn.Params[i] {
				return 0, c.at(arg.Pos(), "argument %d of %q expects %s, got %s", i+1, e.Name, fn.Params[i], ty)
			}
		}
		e.Result = fn.Result
	default:
		return 0, c.at(e.Pos(), "unsupported expression %T", e)
	}
	return exprType(e), nil
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
