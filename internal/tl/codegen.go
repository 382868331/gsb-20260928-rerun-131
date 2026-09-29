package tl

import "fmt"

type genScope struct {
	parent *genScope
	vars   map[string]int
}

type generator struct {
	program  *IRProgram
	current  *IRFunction
	scope    *genScope
	nextSlot int
	indexes  map[string]int
}

func Generate(checked *CheckedProgram) (*IRProgram, error) {
	g := &generator{program: &IRProgram{Version: IRVersion}, indexes: map[string]int{}}
	for i, fn := range checked.Program.Functions {
		g.indexes[fn.Name] = i
	}
	for _, fn := range checked.Program.Functions {
		cf := checked.Functions[fn.Name]
		irFn := IRFunction{Name: fn.Name, Result: fn.Result, LocalSize: cf.LocalSize}
		for _, param := range fn.Params {
			irFn.Params = append(irFn.Params, param.Type)
		}
		g.current = &irFn
		g.scope = &genScope{vars: map[string]int{}}
		g.nextSlot = 0
		for range fn.Params {
			name := fn.Params[g.nextSlot].Name
			g.scope.vars[name] = g.nextSlot
			g.nextSlot++
		}
		if err := g.genBlock(fn.Body); err != nil {
			return nil, err
		}
		g.program.Functions = append(g.program.Functions, *g.current)
	}
	return g.program, nil
}

func (g *generator) emit(op string, arg int64, pos Position) int {
	g.current.Code = append(g.current.Code, Instruction{Op: op, Arg: arg, Pos: pos})
	return len(g.current.Code) - 1
}

func (g *generator) genBlock(block *Block) error {
	old := g.scope
	g.scope = &genScope{parent: old, vars: map[string]int{}}
	defer func() { g.scope = old }()
	for _, stmt := range block.Stmt {
		if err := g.genStatement(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (g *generator) genStatement(stmt Stmt) error {
	switch s := stmt.(type) {
	case *Block:
		return g.genBlock(s)
	case *VarStmt:
		if err := g.genExpression(s.Value); err != nil {
			return err
		}
		slot := g.nextSlot
		g.nextSlot++
		g.scope.vars[s.Name] = slot
		g.emit(OpStoreLocal, int64(slot), s.NamePos)
		return nil
	case *AssignStmt:
		if err := g.genExpression(s.Value); err != nil {
			return err
		}
		slot, ok := g.lookup(s.Name)
		if !ok {
			return errorAt(s.NamePos, "internal error: unresolved variable %q", s.Name)
		}
		g.emit(OpStoreLocal, int64(slot), s.NamePos)
		return nil
	case *IfStmt:
		if err := g.genExpression(s.Condition); err != nil {
			return err
		}
		jumpIfFalse := g.emit(OpJumpIfFalse, 0, s.Pos)
		if err := g.genBlock(s.Then); err != nil {
			return err
		}
		jumpEnd := g.emit(OpJump, 0, s.Pos)
		g.current.Code[jumpIfFalse].Arg = int64(len(g.current.Code))
		if s.Else != nil {
			if err := g.genBlock(s.Else); err != nil {
				return err
			}
		}
		g.current.Code[jumpEnd].Arg = int64(len(g.current.Code))
		return nil
	case *WhileStmt:
		start := len(g.current.Code)
		if err := g.genExpression(s.Condition); err != nil {
			return err
		}
		exit := g.emit(OpJumpIfFalse, 0, s.Pos)
		if err := g.genBlock(s.Body); err != nil {
			return err
		}
		g.emit(OpJump, int64(start), s.Pos)
		g.current.Code[exit].Arg = int64(len(g.current.Code))
		return nil
	case *ReturnStmt:
		if err := g.genExpression(s.Value); err != nil {
			return err
		}
		g.emit(OpReturn, 0, s.Pos)
		return nil
	case *PrintStmt:
		if err := g.genExpression(s.Value); err != nil {
			return err
		}
		g.emit(OpPrint, 0, s.Pos)
		return nil
	case *ExprStmt:
		if err := g.genExpression(s.Value); err != nil {
			return err
		}
		g.emit(OpPop, 0, s.Value.ExpressionPos())
		return nil
	default:
		return fmt.Errorf("unsupported statement %T", stmt)
	}
}

func (g *generator) lookup(name string) (int, bool) {
	for s := g.scope; s != nil; s = s.parent {
		if slot, ok := s.vars[name]; ok {
			return slot, true
		}
	}
	return 0, false
}

func (g *generator) genExpression(expr Expr) error {
	switch e := expr.(type) {
	case *IntegerLit:
		g.emit(OpConstInt, e.Value, e.Pos)
	case *BoolLit:
		arg := 0
		if e.Value {
			arg = 1
		}
		g.emit(OpConstBool, int64(arg), e.Pos)
	case *IdentExpr:
		slot, ok := g.lookup(e.Name)
		if !ok {
			return errorAt(e.Pos, "internal error: unresolved variable %q", e.Name)
		}
		g.emit(OpLoadLocal, int64(slot), e.Pos)
	case *UnaryExpr:
		if err := g.genExpression(e.Value); err != nil {
			return err
		}
		if e.Operator == "-" {
			g.emit(OpNegate, 0, e.Pos)
		} else {
			g.emit(OpNot, 0, e.Pos)
		}
	case *BinaryExpr:
		if err := g.genExpression(e.Left); err != nil {
			return err
		}
		if err := g.genExpression(e.Right); err != nil {
			return err
		}
		op, ok := binaryOpcode(e.Operator)
		if !ok {
			return errorAt(e.Pos, "internal error: unsupported operator %q", e.Operator)
		}
		g.emit(op, 0, e.Pos)
	case *CallExpr:
		for _, arg := range e.Arguments {
			if err := g.genExpression(arg); err != nil {
				return err
			}
		}
		index, ok := g.indexes[e.Name]
		if !ok {
			return errorAt(e.Pos, "internal error: unresolved function %q", e.Name)
		}
		g.emit(OpCall, int64(index), e.Pos)
	default:
		return fmt.Errorf("unsupported expression %T", expr)
	}
	return nil
}

func binaryOpcode(op string) (string, bool) {
	switch op {
	case "+":
		return OpAdd, true
	case "-":
		return OpSubtract, true
	case "*":
		return OpMultiply, true
	case "/":
		return OpDivide, true
	case "==":
		return OpEqual, true
	case "!=":
		return OpNotEqual, true
	case "<":
		return OpLess, true
	case "<=":
		return OpLessEqual, true
	case ">":
		return OpGreater, true
	case ">=":
		return OpGreaterEqual, true
	case "&&":
		return OpAnd, true
	case "||":
		return OpOr, true
	default:
		return "", false
	}
}
