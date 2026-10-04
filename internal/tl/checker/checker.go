package checker

import (
	"fmt"
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
	RootBase  int
	SelfSlot  int
	Builtin   bool
	FuncLit   *ast.FuncLit
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
		c.funcs[fn.Name] = &FuncInfo{Name: fn.Name, Params: params, Result: fn.Result, Index: i}
		c.order = append(c.order, fn.Name)
	}
	mainInfo, ok := c.funcs["main"]
	if !ok {
		return nil, c.at(token.Pos{Line: 1, Column: 1}, "program must declare function main() -> int")
	}
	if len(mainInfo.Params) != 0 || !ast.SameType(mainInfo.Result, ast.TInt) {
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
	if err := c.checkSignature(fn.Params, fn.Body, false); err != nil {
		return err
	}
	if !blockAlwaysReturns(fn.Body) {
		return c.at(fn.Body.Posn, "function %q must return a value on every path", fn.Name)
	}
	c.curFunc.NumLocals = c.nextSlot
	c.scopes = nil
	c.curFunc = nil
	return nil
}

func (c *Checker) checkSignature(params []*ast.Param, body *ast.Block, newScope bool) error {
	seen := map[string]bool{}
	for _, p := range params {
		if seen[p.Name] {
			return c.at(p.Posn, "duplicate parameter %q", p.Name)
		}
		seen[p.Name] = true
		if _, err := c.declare(p.Name, p.Type, p.Posn); err != nil {
			return err
		}
	}
	return c.checkBlock(body, newScope)
}

func (c *Checker) checkBlock(b *ast.Block, newScope bool) error {
	start := c.nextSlot
	if newScope {
		b.SlotBase = start
		c.pushScope()
		defer c.popScope()
	}
	if !newScope {
		b.SlotBase = 0
	}
	for _, s := range b.Stmts {
		if err := c.checkStmt(s); err != nil {
			return err
		}
	}
	b.ScopeSize = c.nextSlot - start
	return nil
}

func (c *Checker) checkStmt(s ast.Stmt) error {
	switch s := s.(type) {
	case *ast.LetStmt:
		slot, err := c.declare(s.Name, s.Type, s.Posn)
		if err != nil {
			return err
		}
		if s.Init == nil {
			return c.at(s.Posn, "variable %q must be initialized", s.Name)
		}
		ty, err := c.checkExpr(s.Init)
		if err != nil {
			return err
		}
		if !ast.SameType(ty, s.Type) {
			return c.at(s.Posn, "cannot initialize %s variable %q with %s value", s.Type, s.Name, ty)
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
		if !ast.SameType(ty, b.ty) {
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
		if !ast.SameType(ty, ast.TBool) {
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
		if !ast.SameType(ty, ast.TBool) {
			return c.at(s.Cond.Pos(), "while condition must be bool, got %s", ty)
		}
		return c.checkBlock(s.Body, true)
	case *ast.ReturnStmt:
		ty, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if !ast.SameType(ty, c.curFunc.Result) {
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
		if fn := c.funcs[e.Name]; fn != nil && !fn.Builtin {
			e.FuncIndex = fn.Index
			e.TType = topLevelFuncType(fn)
			break
		}
		b, err := c.lookup(e.Name, e.Posn)
		if err != nil {
			return nil, err
		}
		e.Slot = b.slot
		e.TType = b.ty
	case *ast.FuncLit:
		e.TType = &ast.FuncType{Result: e.Result, Posn: e.Posn}
		for _, p := range e.Params {
			e.TType.(*ast.FuncType).Params = append(e.TType.(*ast.FuncType).Params, p.Type)
		}
		e.Index = len(c.order)
		for _, fn := range c.funcs {
			if fn.FuncLit != nil && fn.Index >= e.Index {
				e.Index = fn.Index + 1
			}
		}
		info := &FuncInfo{Name: e.Name, Params: e.TType.(*ast.FuncType).Params, Result: e.Result, Index: e.Index, FuncLit: e}
		info.SelfSlot = -1
		c.funcs[litKey(e.Index)] = info
		parentScopes, parentNext, parentFunc := c.scopes, c.nextSlot, c.curFunc
		info.RootBase = c.nextSlot
		c.nextSlot = info.RootBase + len(e.Params)
		c.scopes, c.nextSlot, c.curFunc = nil, info.RootBase, info
		c.pushScope()
		c.scopes.parent = parentScopes
		for _, p := range e.Params {
			if _, err := c.declare(p.Name, p.Type, p.Posn); err != nil {
				return nil, err
			}
		}
		if e.Name != "" {
			e.SelfSlot = info.RootBase - 1
			info.SelfSlot = e.SelfSlot
			c.scopes.vars[e.Name] = binding{slot: e.SelfSlot, ty: e.TType}
		}
		err := c.checkBlock(e.Body, false)
		if err == nil && !blockAlwaysReturns(e.Body) {
			err = c.at(e.Body.Posn, "function must return a value on every path")
		}
		if err == nil {
			info.NumLocals = c.nextSlot
		}
		nextAfter := info.NumLocals
		if err != nil {
			nextAfter = parentNext
		}
		c.scopes, c.nextSlot, c.curFunc = parentScopes, nextAfter, parentFunc
		if err != nil {
			return nil, err
		}
	case *ast.UnaryExpr:
		t, err := c.checkExpr(e.X)
		if err != nil {
			return nil, err
		}
		switch e.Op {
		case token.MINUS:
			if !ast.SameType(t, ast.TInt) {
				return nil, c.at(e.Posn, "unary '-' requires int, got %s", t)
			}
		case token.BANG:
			if !ast.SameType(t, ast.TBool) {
				return nil, c.at(e.Posn, "unary '!' requires bool, got %s", t)
			}
		}
		e.TType = t
	case *ast.BinaryExpr:
		lt, err := c.checkExpr(e.L)
		if err != nil {
			return nil, err
		}
		rt, err := c.checkExpr(e.R)
		if err != nil {
			return nil, err
		}
		if !ast.SameType(lt, rt) {
			return nil, c.at(e.Posn, "type mismatch in binary expression: %s vs %s", lt, rt)
		}
		switch e.Op {
		case token.EQ, token.NE:
			return nil, c.at(e.Posn, "functions cannot be compared for equality")
		case token.LT, token.GT, token.LE, token.GE:
			if !ast.SameType(lt, ast.TInt) {
				return nil, c.at(e.Posn, "ordering comparison requires int operands")
			}
			e.TType = ast.TBool
		case token.AND, token.OR:
			if !ast.SameType(lt, ast.TBool) {
				return nil, c.at(e.Posn, "logical operator requires bool operands")
			}
			e.TType = ast.TBool
		default:
			if !ast.SameType(lt, ast.TInt) {
				return nil, c.at(e.Posn, "arithmetic operator requires int operands")
			}
			e.TType = ast.TInt
		}
	case *ast.CallExpr:
		if e.Name != "" {
			if id, ok := e.Callee.(*ast.Ident); ok {
				if b, err := c.lookup(id.Name, id.Posn); err == nil {
					id.Slot = b.slot
					id.TType = b.ty
					e.Name = ""
					e.Dynamic = true
					fnTy, ok := b.ty.(*ast.FuncType)
					if !ok {
						return nil, c.at(e.Posn, "cannot call non-function value of type %s", b.ty)
					}
					if err := c.checkArguments(e, fnTy.Params, fnTy.Result); err != nil {
						return nil, err
					}
					break
				}
			}
			fn := c.funcs[e.Name]
			if fn == nil {
				return nil, c.at(e.Posn, "undefined function %q", e.Name)
			}
			if fn.Builtin {
				e.Builtin = e.Name
			}
			if err := c.checkArguments(e, fn.Params, fn.Result); err != nil {
				return nil, err
			}
			break
		}
		calleeTy, err := c.checkExpr(e.Callee)
		if err != nil {
			return nil, err
		}
		fnTy, ok := calleeTy.(*ast.FuncType)
		if !ok {
			return nil, c.at(e.Posn, "cannot call non-function value of type %s", calleeTy)
		}
		if err := c.checkArguments(e, fnTy.Params, fnTy.Result); err != nil {
			return nil, err
		}
	default:
		return nil, c.at(e.Pos(), "unsupported expression %T", e)
	}
	return exprType(e), nil
}

func (c *Checker) checkArguments(e *ast.CallExpr, params []ast.Type, result ast.Type) error {
	if len(e.Args) != len(params) {
		return c.at(e.Posn, "function expects %d arguments, got %d", len(params), len(e.Args))
	}
	for i, arg := range e.Args {
		ty, err := c.checkExpr(arg)
		if err != nil {
			return err
		}
		if !ast.SameType(ty, params[i]) {
			return c.at(arg.Pos(), "argument %d expects %s, got %s", i+1, params[i], ty)
		}
	}
	e.Result = result
	return nil
}

func topLevelFuncType(fn *FuncInfo) *ast.FuncType {
	return &ast.FuncType{Params: fn.Params, Result: fn.Result}
}

func litKey(index int) string { return fmt.Sprintf("#lit%d", index) }

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
	case *ast.FuncLit:
		return e.TType
	default:
		return nil
	}
}

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
