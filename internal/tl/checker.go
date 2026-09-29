package tl

import "fmt"

type variableInfo struct {
	typ  Type
	slot int
}

type scope struct {
	parent *scope
	vars   map[string]variableInfo
}

func (s *scope) declare(name string, info variableInfo, pos Position) error {
	if _, exists := s.vars[name]; exists {
		return errorAt(pos, "variable %q is already declared in this scope", name)
	}
	s.vars[name] = info
	return nil
}

func (s *scope) lookup(name string) (variableInfo, bool) {
	for current := s; current != nil; current = current.parent {
		if info, ok := current.vars[name]; ok {
			return info, true
		}
	}
	return variableInfo{}, false
}

type functionInfo struct {
	fn   *Function
	slot int
}

type CheckedProgram struct {
	Program   *Program
	Functions map[string]*CheckedFunction
}

type CheckedFunction struct {
	Node      *Function
	Locals    map[string]variableInfo
	LocalSize int
}

func Check(program *Program) (*CheckedProgram, error) {
	c := &checker{functions: map[string]*functionInfo{}, checked: &CheckedProgram{Program: program, Functions: map[string]*CheckedFunction{}}}
	for _, fn := range program.Functions {
		if _, exists := c.functions[fn.Name]; exists {
			return nil, errorAt(fn.NamePos, "function %q is already declared", fn.Name)
		}
		c.functions[fn.Name] = &functionInfo{fn: fn}
	}
	main, ok := c.functions["main"]
	if !ok {
		return nil, errorAt(Position{Line: 1, Col: 1}, "program must define main")
	}
	if len(main.fn.Params) != 0 || main.fn.Result != IntType {
		return nil, errorAt(main.fn.NamePos, "main must be fn main() -> int")
	}
	for _, fn := range program.Functions {
		cf, err := c.checkFunction(fn)
		if err != nil {
			return nil, err
		}
		c.checked.Functions[fn.Name] = cf
	}
	return c.checked, nil
}

type checker struct {
	functions map[string]*functionInfo
	current   *Function
	scope     *scope
	nextSlot  int
	checked   *CheckedProgram
}

func (c *checker) checkFunction(fn *Function) (*CheckedFunction, error) {
	c.current = fn
	c.scope = &scope{vars: map[string]variableInfo{}}
	c.nextSlot = 0
	seen := map[string]bool{}
	for _, param := range fn.Params {
		if seen[param.Name] {
			return nil, errorAt(param.Pos, "duplicate parameter %q", param.Name)
		}
		seen[param.Name] = true
		if err := c.scope.declare(param.Name, variableInfo{typ: param.Type, slot: c.nextSlot}, param.Pos); err != nil {
			return nil, err
		}
		c.nextSlot++
	}
	if err := c.checkBlock(fn.Body); err != nil {
		return nil, err
	}
	if !c.blockAlwaysReturns(fn.Body) {
		return nil, errorAt(fn.ResultPos, "function %q may reach end without returning %s", fn.Name, fn.Result)
	}
	return &CheckedFunction{Node: fn, Locals: copyLocals(c.scope), LocalSize: c.nextSlot}, nil
}

func copyLocals(root *scope) map[string]variableInfo {
	result := map[string]variableInfo{}
	var scopes []*scope
	for s := root; s != nil; s = s.parent {
		scopes = append([]*scope{s}, scopes...)
	}
	for _, s := range scopes {
		for name, info := range s.vars {
			result[name] = info
		}
	}
	return result
}

func (c *checker) checkBlock(block *Block) error {
	old := c.scope
	c.scope = &scope{parent: old, vars: map[string]variableInfo{}}
	defer func() { c.scope = old }()
	for _, stmt := range block.Stmt {
		if err := c.checkStatement(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *checker) checkStatement(stmt Stmt) error {
	switch s := stmt.(type) {
	case *Block:
		return c.checkBlock(s)
	case *VarStmt:
		valueType, err := c.checkExpression(s.Value)
		if err != nil {
			return err
		}
		if s.VarType != "" && s.VarType != valueType {
			return errorAt(s.NamePos, "cannot initialize %s variable with %s", s.VarType, valueType)
		}
		info := variableInfo{typ: valueType, slot: c.nextSlot}
		if err := c.scope.declare(s.Name, info, s.NamePos); err != nil {
			return err
		}
		c.nextSlot++
		return nil
	case *AssignStmt:
		info, ok := c.scope.lookup(s.Name)
		if !ok {
			return errorAt(s.NamePos, "undefined variable %q", s.Name)
		}
		valueType, err := c.checkExpression(s.Value)
		if err != nil {
			return err
		}
		if valueType != info.typ {
			return errorAt(s.NamePos, "cannot assign %s to %s variable %q", valueType, info.typ, s.Name)
		}
		return nil
	case *IfStmt:
		if err := c.expectType(s.Condition, BoolType); err != nil {
			return err
		}
		if err := c.checkBlock(s.Then); err != nil {
			return err
		}
		if s.Else != nil {
			return c.checkBlock(s.Else)
		}
		return nil
	case *WhileStmt:
		if err := c.expectType(s.Condition, BoolType); err != nil {
			return err
		}
		return c.checkBlock(s.Body)
	case *ReturnStmt:
		actual, err := c.checkExpression(s.Value)
		if err != nil {
			return err
		}
		if actual != c.current.Result {
			return errorAt(s.Pos, "cannot return %s from function returning %s", actual, c.current.Result)
		}
		return nil
	case *PrintStmt:
		_, err := c.checkExpression(s.Value)
		return err
	case *ExprStmt:
		_, err := c.checkExpression(s.Value)
		return err
	default:
		return fmt.Errorf("unsupported statement %T", stmt)
	}
}

func (c *checker) expectType(expr Expr, expected Type) error {
	actual, err := c.checkExpression(expr)
	if err != nil {
		return err
	}
	if actual != expected {
		return errorAt(expr.ExpressionPos(), "expected %s, got %s", expected, actual)
	}
	return nil
}

func (c *checker) checkExpression(expr Expr) (Type, error) {
	switch e := expr.(type) {
	case *IntegerLit:
		return IntType, nil
	case *BoolLit:
		return BoolType, nil
	case *IdentExpr:
		info, ok := c.scope.lookup(e.Name)
		if !ok {
			return "", errorAt(e.Pos, "undefined variable %q", e.Name)
		}
		return info.typ, nil
	case *UnaryExpr:
		inner, err := c.checkExpression(e.Value)
		if err != nil {
			return "", err
		}
		if e.Operator == "-" && inner != IntType {
			return "", errorAt(e.Pos, "unary - requires int")
		}
		if e.Operator == "!" && inner != BoolType {
			return "", errorAt(e.Pos, "unary ! requires bool")
		}
		return inner, nil
	case *BinaryExpr:
		return c.checkBinary(e)
	case *CallExpr:
		fn, ok := c.functions[e.Name]
		if !ok {
			return "", errorAt(e.Pos, "unknown function %q", e.Name)
		}
		if len(e.Arguments) != len(fn.fn.Params) {
			return "", errorAt(e.Pos, "function %q expects %d arguments, got %d", e.Name, len(fn.fn.Params), len(e.Arguments))
		}
		for i, arg := range e.Arguments {
			actual, err := c.checkExpression(arg)
			if err != nil {
				return "", err
			}
			expected := fn.fn.Params[i].Type
			if actual != expected {
				return "", errorAt(arg.ExpressionPos(), "argument %d of %q expects %s, got %s", i+1, e.Name, expected, actual)
			}
		}
		return fn.fn.Result, nil
	default:
		return "", fmt.Errorf("unsupported expression %T", expr)
	}
}

func (c *checker) checkBinary(e *BinaryExpr) (Type, error) {
	left, err := c.checkExpression(e.Left)
	if err != nil {
		return "", err
	}
	right, err := c.checkExpression(e.Right)
	if err != nil {
		return "", err
	}
	if left != right {
		return "", errorAt(e.Pos, "operands must have the same type, got %s and %s", left, right)
	}
	switch e.Operator {
	case "&&", "||":
		if left != BoolType {
			return "", errorAt(e.Pos, "%s requires bool operands", e.Operator)
		}
		return BoolType, nil
	case "==", "!=":
		return BoolType, nil
	case "<", "<=", ">", ">=":
		if left != IntType {
			return "", errorAt(e.Pos, "%s requires int operands", e.Operator)
		}
		return BoolType, nil
	case "+", "-", "*", "/":
		if left != IntType {
			return "", errorAt(e.Pos, "%s requires int operands", e.Operator)
		}
		return IntType, nil
	default:
		return "", errorAt(e.Pos, "unsupported operator %q", e.Operator)
	}
}

func (c *checker) blockAlwaysReturns(block *Block) bool {
	for _, stmt := range block.Stmt {
		if c.stmtAlwaysReturns(stmt) {
			return true
		}
	}
	return false
}

func (c *checker) stmtAlwaysReturns(stmt Stmt) bool {
	switch s := stmt.(type) {
	case *ReturnStmt:
		return true
	case *Block:
		return c.blockAlwaysReturns(s)
	case *IfStmt:
		return s.Else != nil && c.blockAlwaysReturns(s.Then) && c.blockAlwaysReturns(s.Else)
	default:
		return false
	}
}
