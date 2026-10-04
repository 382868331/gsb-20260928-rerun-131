package codegen

import (
	"fmt"
	"tl/internal/tl/ast"
	"tl/internal/tl/checker"
	"tl/internal/tl/ir"
	"tl/internal/tl/token"
)

type Generator struct {
	prog *ir.Program
	fn   *ir.Function
	info *checker.FuncInfo
	all  map[string]*checker.FuncInfo
}

func Generate(parsed *ast.Program, checked *checker.Result) *ir.Program {
	g := &Generator{prog: &ir.Program{Main: uint32(checked.Funcs["main"].Index)}, all: checked.Funcs}
	for _, fn := range parsed.Funcs {
		meta := checked.Funcs[fn.Name]
		g.beginFunction(meta)
		g.block(fn.Body, false)
		g.emit(ir.OpHalt, 0, fn.Body.Posn.Line)
		g.prog.Funcs = append(g.prog.Funcs, g.fn)
	}
	lits := map[int]*ast.FuncLit{}
	for _, fn := range parsed.Funcs {
		collectFuncLits(fn.Body, lits)
	}
	for len(lits) > 0 {
		next := -1
		for index := range lits {
			if next == -1 || index < next {
				next = index
			}
		}
		lit := lits[next]
		delete(lits, next)
		meta := g.all[litKey(next)]
		g.beginFunction(meta)
		g.block(lit.Body, false)
		g.emit(ir.OpHalt, 0, lit.Body.Posn.Line)
		g.prog.Funcs = append(g.prog.Funcs, g.fn)
	}
	return g.prog
}

func litKey(index int) string {
	return "#lit" + fmt.Sprint(index)
}

func collectFuncLits(b *ast.Block, found map[int]*ast.FuncLit) {
	for _, s := range b.Stmts {
		switch s := s.(type) {
		case *ast.LetStmt:
			collectExprFuncLits(s.Init, found)
		case *ast.AssignStmt:
			collectExprFuncLits(s.Value, found)
		case *ast.ExprStmt:
			collectExprFuncLits(s.X, found)
		case *ast.IfStmt:
			collectExprFuncLits(s.Cond, found)
			collectFuncLits(s.Then, found)
			if e, ok := s.Else.(*ast.BlockStmt); ok {
				collectFuncLits(e.Block, found)
			}
		case *ast.WhileStmt:
			collectExprFuncLits(s.Cond, found)
			collectFuncLits(s.Body, found)
		case *ast.ReturnStmt:
			collectExprFuncLits(s.Value, found)
		case *ast.BlockStmt:
			collectFuncLits(s.Block, found)
		}
	}
}

func collectExprFuncLits(e ast.Expr, found map[int]*ast.FuncLit) {
	switch e := e.(type) {
	case *ast.FuncLit:
		found[e.Index] = e
		collectFuncLits(e.Body, found)
	case *ast.UnaryExpr:
		collectExprFuncLits(e.X, found)
	case *ast.BinaryExpr:
		collectExprFuncLits(e.L, found)
		collectExprFuncLits(e.R, found)
	case *ast.CallExpr:
		collectExprFuncLits(e.Callee, found)
		for _, arg := range e.Args {
			collectExprFuncLits(arg, found)
		}
	}
}

func (g *Generator) beginFunction(meta *checker.FuncInfo) {
	g.info = meta
	g.fn = &ir.Function{
		Name:      meta.Name,
		Params:    uint32(len(meta.Params)),
		NumLocals: uint32(meta.NumLocals),
		RootBase:  uint32(meta.RootBase),
		SelfSlot:  -1,
	}
	if meta.FuncLit != nil {
		g.fn.Name = meta.FuncLit.Name
		g.fn.SelfSlot = int32(meta.SelfSlot)
	}
}

func (g *Generator) emit(op ir.Op, arg uint32, line int) int {
	idx := len(g.fn.Code)
	g.fn.Code = append(g.fn.Code, ir.Instr{Op: op, Arg: arg, Line: uint32(line)})
	return idx
}

func (g *Generator) block(b *ast.Block, newScope bool) {
	if newScope {
		g.emit(ir.OpEnterScope, uint32(b.SlotBase), b.Posn.Line)
	}
	for _, s := range b.Stmts {
		g.stmt(s)
	}
	if newScope {
		g.emit(ir.OpLeaveScope, uint32(b.SlotBase), b.Posn.Line)
	}
}

func (g *Generator) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.LetStmt:
		g.expr(s.Init)
		g.emit(ir.OpStore, uint32(s.Slot), s.Posn.Line)
	case *ast.AssignStmt:
		g.expr(s.Value)
		g.emit(ir.OpStore, uint32(s.Slot), s.Posn.Line)
	case *ast.ExprStmt:
		g.expr(s.X)
		g.emit(ir.OpPop, 0, s.Posn.Line)
	case *ast.IfStmt:
		g.expr(s.Cond)
		jumpEnd := g.emit(ir.OpPopJumpIfFalse, 0, s.Posn.Line)
		g.block(s.Then, true)
		if s.Else != nil {
			jumpElse := g.emit(ir.OpJump, 0, s.Posn.Line)
			g.fn.Code[jumpEnd].Arg = uint32(len(g.fn.Code))
			g.stmt(s.Else)
			g.fn.Code[jumpElse].Arg = uint32(len(g.fn.Code))
		} else {
			g.fn.Code[jumpEnd].Arg = uint32(len(g.fn.Code))
		}
	case *ast.WhileStmt:
		start := len(g.fn.Code)
		g.emit(ir.OpEnterScope, uint32(s.Body.SlotBase), s.Posn.Line)
		g.expr(s.Cond)
		exit := g.emit(ir.OpPopJumpIfFalse, 0, s.Posn.Line)
		g.block(s.Body, false)
		g.emit(ir.OpLeaveScope, uint32(s.Body.SlotBase), s.Posn.Line)
		g.emit(ir.OpJump, uint32(start), s.Posn.Line)
		g.emit(ir.OpLeaveScope, uint32(s.Body.SlotBase), s.Posn.Line)
		g.fn.Code[exit].Arg = uint32(len(g.fn.Code))
	case *ast.ReturnStmt:
		g.expr(s.Value)
		g.emit(ir.OpReturn, 0, s.Posn.Line)
	case *ast.BlockStmt:
		g.block(s.Block, true)
	}
}

func (g *Generator) expr(e ast.Expr) {
	switch e := e.(type) {
	case *ast.IntLit:
		g.emit(ir.OpConst, g.fn.AddConst(e.Value), e.Posn.Line)
	case *ast.BoolLit:
		v := int64(0)
		if e.Value {
			v = 1
		}
		g.emit(ir.OpConst, g.fn.AddConst(v), e.Posn.Line)
	case *ast.Ident:
		if e.FuncIndex >= 0 && e.Slot < 0 {
			g.emit(ir.OpClosure, uint32(e.FuncIndex), e.Posn.Line)
			return
		}
		g.emit(ir.OpLoad, uint32(e.Slot), e.Posn.Line)
	case *ast.FuncLit:
		g.emit(ir.OpClosure, uint32(e.Index), e.Posn.Line)
	case *ast.UnaryExpr:
		g.expr(e.X)
		if e.Op == token.MINUS {
			g.emit(ir.OpNeg, 0, e.Posn.Line)
		} else {
			g.emit(ir.OpNot, 0, e.Posn.Line)
		}
	case *ast.BinaryExpr:
		if e.Op == token.AND || e.Op == token.OR {
			g.expr(e.L)
			jump := g.emit(ir.OpJumpIfFalse, 0, e.Posn.Line)
			if e.Op == token.OR {
				jump = g.emit(ir.OpJumpIfTrue, 0, e.Posn.Line)
			}
			g.emit(ir.OpPop, 0, e.Posn.Line)
			g.expr(e.R)
			g.fn.Code[jump].Arg = uint32(len(g.fn.Code))
			return
		}
		g.expr(e.L)
		g.expr(e.R)
		g.emit(binaryOp(e.Op), 0, e.Posn.Line)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			g.expr(arg)
		}
		if e.Builtin != "" {
			id := uint32(0)
			if e.Builtin == "print_bool" {
				id = 1
			}
			g.emit(ir.OpCallBuiltin, id, e.Posn.Line)
			return
		}
		if e.Dynamic || e.Name == "" {
			g.expr(e.Callee)
			g.emit(ir.OpCallValue, uint32(len(e.Args)), e.Posn.Line)
			return
		}
		meta := g.all[e.Name]
		g.emit(ir.OpCall, uint32(meta.Index), e.Posn.Line)
	}
}

func binaryOp(k token.Kind) ir.Op {
	switch k {
	case token.PLUS:
		return ir.OpAdd
	case token.MINUS:
		return ir.OpSub
	case token.STAR:
		return ir.OpMul
	case token.SLASH:
		return ir.OpDiv
	case token.PERCENT:
		return ir.OpMod
	case token.EQ:
		return ir.OpEq
	case token.NE:
		return ir.OpNe
	case token.LT:
		return ir.OpLt
	case token.GT:
		return ir.OpGt
	case token.LE:
		return ir.OpLe
	case token.GE:
		return ir.OpGe
	default:
		return ir.OpHalt
	}
}
