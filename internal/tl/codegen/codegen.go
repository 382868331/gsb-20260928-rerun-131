package codegen

import (
	"tl/internal/tl/ast"
	"tl/internal/tl/checker"
	"tl/internal/tl/ir"
	"tl/internal/tl/token"
)

type Generator struct {
	prog    *ir.Program
	fn      *ir.Function
	info    map[string]*checker.FuncInfo
	results *checker.Result
}

func Generate(parsed *ast.Program, checked *checker.Result) *ir.Program {
	p := &ir.Program{Main: uint32(checked.Funcs["main"].Index)}
	g := &Generator{prog: p, info: checked.Funcs, results: checked}
	for _, fn := range parsed.Funcs {
		meta := checked.Funcs[fn.Name]
		g.fn = &ir.Function{
			Name:      fn.Name,
			Params:    uint32(len(fn.Params)),
			NumLocals: uint32(meta.NumLocals),
		}
		g.block(fn.Body)
		// checkFunc guarantees a return, but emit a defensive halt so invalid
		// internal state never falls through.
		g.emit(ir.OpHalt, 0, fn.Body.Posn.Line)
		p.Funcs = append(p.Funcs, g.fn)
	}
	return p
}

func (g *Generator) emit(op ir.Op, arg uint32, line int) int {
	idx := len(g.fn.Code)
	g.fn.Code = append(g.fn.Code, ir.Instr{Op: op, Arg: arg, Line: uint32(line)})
	return idx
}

func (g *Generator) block(b *ast.Block) {
	for _, s := range b.Stmts {
		g.stmt(s)
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
		g.block(s.Then)
		if s.Else != nil {
			jumpElseEnd := g.emit(ir.OpJump, 0, s.Posn.Line)
			g.fn.Code[jumpEnd].Arg = uint32(len(g.fn.Code))
			g.stmt(s.Else)
			g.fn.Code[jumpElseEnd].Arg = uint32(len(g.fn.Code))
		} else {
			g.fn.Code[jumpEnd].Arg = uint32(len(g.fn.Code))
		}
	case *ast.WhileStmt:
		start := len(g.fn.Code)
		g.expr(s.Cond)
		exit := g.emit(ir.OpPopJumpIfFalse, 0, s.Posn.Line)
		g.block(s.Body)
		g.emit(ir.OpJump, uint32(start), s.Posn.Line)
		g.fn.Code[exit].Arg = uint32(len(g.fn.Code))
	case *ast.ReturnStmt:
		g.expr(s.Value)
		g.emit(ir.OpReturn, 0, s.Posn.Line)
	case *ast.BlockStmt:
		g.block(s.Block)
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
		g.emit(ir.OpLoad, uint32(e.Slot), e.Posn.Line)
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
			var jump int
			if e.Op == token.AND {
				jump = g.emit(ir.OpJumpIfFalse, 0, e.Posn.Line)
			} else {
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
		meta := g.info[e.Name]
		for _, arg := range e.Args {
			g.expr(arg)
		}
		if meta.Builtin {
			id := uint32(0)
			if e.Name == "print_bool" {
				id = 1
			}
			g.emit(ir.OpCallBuiltin, id, e.Posn.Line)
		} else {
			g.emit(ir.OpCall, uint32(meta.Index), e.Posn.Line)
		}
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
