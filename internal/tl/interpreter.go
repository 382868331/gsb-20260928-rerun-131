package tl

import (
	"fmt"
	"io"
	"math"
)

type interpreterValue struct {
	typ Type
	i   int64
	b   bool
}

type interpreterEnv struct {
	parent *interpreterEnv
	vars   map[string]interpreterValue
}

type Interpreter struct {
	program *CheckedProgram
	budget  int64
	depth   int
	out     io.Writer
}

func NewInterpreter(program *CheckedProgram, budget int64, out io.Writer) (*Interpreter, error) {
	if program == nil {
		return nil, fmt.Errorf("checked program is required")
	}
	if budget <= 0 {
		return nil, fmt.Errorf("budget must be positive")
	}
	return &Interpreter{program: program, budget: budget, out: out}, nil
}

func (it *Interpreter) Run() (int64, error) {
	value, err := it.call("main", nil, Position{Line: 1, Col: 1})
	if err != nil {
		return 0, err
	}
	return value.i, nil
}

func (it *Interpreter) call(name string, args []interpreterValue, pos Position) (interpreterValue, error) {
	cf, ok := it.program.Functions[name]
	if !ok {
		return interpreterValue{}, &RuntimeError{Pos: pos, Msg: fmt.Sprintf("unknown function %q", name)}
	}
	if it.depth >= MaxCallDepth {
		return interpreterValue{}, &RuntimeError{Pos: pos, Msg: "maximum call depth exceeded"}
	}
	it.depth++
	defer func() { it.depth-- }()
	env := &interpreterEnv{vars: map[string]interpreterValue{}}
	for i, param := range cf.Node.Params {
		env.vars[param.Name] = args[i]
	}
	value, returned, err := it.executeBlock(cf.Node.Body, env)
	if err != nil {
		return interpreterValue{}, err
	}
	if !returned {
		return interpreterValue{}, &RuntimeError{Pos: cf.Node.ResultPos, Msg: fmt.Sprintf("function %q did not return", name)}
	}
	return value, nil
}

func (it *Interpreter) executeBlock(block *Block, parent *interpreterEnv) (interpreterValue, bool, error) {
	env := &interpreterEnv{parent: parent, vars: map[string]interpreterValue{}}
	for _, stmt := range block.Stmt {
		value, returned, err := it.executeStatement(stmt, env)
		if err != nil || returned {
			return value, returned, err
		}
	}
	return interpreterValue{}, false, nil
}

func (it *Interpreter) executeStatement(stmt Stmt, env *interpreterEnv) (interpreterValue, bool, error) {
	it.budget--
	if it.budget < 0 {
		return interpreterValue{}, false, &RuntimeError{Pos: stmt.StatementPos(), Msg: "run budget exhausted"}
	}
	switch s := stmt.(type) {
	case *Block:
		_, returned, err := it.executeBlock(s, env)
		return interpreterValue{}, returned, err
	case *VarStmt:
		value, err := it.evaluate(s.Value, env)
		if err != nil {
			return interpreterValue{}, false, err
		}
		env.vars[s.Name] = value
	case *AssignStmt:
		value, err := it.evaluate(s.Value, env)
		if err != nil {
			return interpreterValue{}, false, err
		}
		if !assign(env, s.Name, value) {
			return interpreterValue{}, false, &RuntimeError{Pos: s.NamePos, Msg: fmt.Sprintf("undefined variable %q", s.Name)}
		}
	case *IfStmt:
		cond, err := it.evaluate(s.Condition, env)
		if err != nil {
			return interpreterValue{}, false, err
		}
		if cond.b {
			return it.executeBlock(s.Then, env)
		}
		if s.Else != nil {
			return it.executeBlock(s.Else, env)
		}
	case *WhileStmt:
		for {
			it.budget--
			if it.budget < 0 {
				return interpreterValue{}, false, &RuntimeError{Pos: s.Pos, Msg: "run budget exhausted"}
			}
			cond, err := it.evaluate(s.Condition, env)
			if err != nil {
				return interpreterValue{}, false, err
			}
			if !cond.b {
				break
			}
			_, returned, err := it.executeBlock(s.Body, env)
			if err != nil || returned {
				return interpreterValue{}, returned, err
			}
		}
	case *ReturnStmt:
		value, err := it.evaluate(s.Value, env)
		if err != nil {
			return interpreterValue{}, false, err
		}
		return value, true, nil
	case *PrintStmt:
		value, err := it.evaluate(s.Value, env)
		if err != nil {
			return interpreterValue{}, false, err
		}
		if value.typ == IntType {
			_, _ = fmt.Fprintln(it.out, value.i)
		} else {
			_, _ = fmt.Fprintln(it.out, value.b)
		}
	case *ExprStmt:
		if _, err := it.evaluate(s.Value, env); err != nil {
			return interpreterValue{}, false, err
		}
	}
	return interpreterValue{}, false, nil
}

func assign(env *interpreterEnv, name string, value interpreterValue) bool {
	for e := env; e != nil; e = e.parent {
		if _, ok := e.vars[name]; ok {
			e.vars[name] = value
			return true
		}
	}
	return false
}

func lookup(env *interpreterEnv, name string) (interpreterValue, bool) {
	for e := env; e != nil; e = e.parent {
		if value, ok := e.vars[name]; ok {
			return value, true
		}
	}
	return interpreterValue{}, false
}

func (it *Interpreter) evaluate(expr Expr, env *interpreterEnv) (interpreterValue, error) {
	switch e := expr.(type) {
	case *IntegerLit:
		return interpreterValue{typ: IntType, i: e.Value}, nil
	case *BoolLit:
		return interpreterValue{typ: BoolType, b: e.Value}, nil
	case *IdentExpr:
		value, ok := lookup(env, e.Name)
		if !ok {
			return interpreterValue{}, &RuntimeError{Pos: e.Pos, Msg: fmt.Sprintf("undefined variable %q", e.Name)}
		}
		return value, nil
	case *UnaryExpr:
		value, err := it.evaluate(e.Value, env)
		if err != nil {
			return interpreterValue{}, err
		}
		if e.Operator == "-" {
			return interpreterValue{typ: IntType, i: -value.i}, nil
		}
		return interpreterValue{typ: BoolType, b: !value.b}, nil
	case *BinaryExpr:
		return it.evaluateBinary(e, env)
	case *CallExpr:
		args := make([]interpreterValue, len(e.Arguments))
		for i, arg := range e.Arguments {
			value, err := it.evaluate(arg, env)
			if err != nil {
				return interpreterValue{}, err
			}
			args[i] = value
		}
		value, err := it.call(e.Name, args, e.Pos)
		if err != nil {
			return interpreterValue{}, err
		}
		return value, nil
	default:
		return interpreterValue{}, fmt.Errorf("unsupported expression %T", expr)
	}
}

func (it *Interpreter) evaluateBinary(e *BinaryExpr, env *interpreterEnv) (interpreterValue, error) {
	left, err := it.evaluate(e.Left, env)
	if err != nil {
		return interpreterValue{}, err
	}
	right, err := it.evaluate(e.Right, env)
	if err != nil {
		return interpreterValue{}, err
	}
	switch e.Operator {
	case "+", "-", "*", "/":
		if right.i == 0 && e.Operator == "/" {
			return interpreterValue{}, &RuntimeError{Pos: e.Pos, Msg: "integer division by zero"}
		}
		var value int64
		switch e.Operator {
		case "+":
			value = left.i + right.i
		case "-":
			value = left.i - right.i
		case "*":
			value = left.i * right.i
		case "/":
			if left.i == math.MinInt64 && right.i == -1 {
				value = math.MinInt64
			} else {
				value = left.i / right.i
			}
		}
		return interpreterValue{typ: IntType, i: value}, nil
	case "==", "!=":
		value := left.i == right.i
		if left.typ == BoolType {
			value = left.b == right.b
		}
		if e.Operator == "!=" {
			value = !value
		}
		return interpreterValue{typ: BoolType, b: value}, nil
	case "<", "<=", ">", ">=":
		var value bool
		switch {
		case e.Operator == "<":
			value = left.i < right.i
		case e.Operator == "<=":
			value = left.i <= right.i
		case e.Operator == ">":
			value = left.i > right.i
		default:
			value = left.i >= right.i
		}
		return interpreterValue{typ: BoolType, b: value}, nil
	case "&&", "||":
		value := left.b && right.b
		if e.Operator == "||" {
			value = left.b || right.b
		}
		return interpreterValue{typ: BoolType, b: value}, nil
	default:
		return interpreterValue{}, &RuntimeError{Pos: e.Pos, Msg: "unsupported operator"}
	}
}
