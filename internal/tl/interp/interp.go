package interp

import (
	"fmt"
	"io"
	"math/bits"

	"tl/internal/tl/ast"
	"tl/internal/tl/tlerr"
	"tl/internal/tl/token"
)

const DefaultBudget = 10_000_000
const MaxCallDepth = 1000

type returnValue struct{ value int64 }

func (*returnValue) Error() string { return "return" }

type env struct {
	parent *env
	vars   map[string]int64
}

type Interpreter struct {
	file   string
	prog   *ast.Program
	funcs  map[string]*ast.FuncDecl
	out    io.Writer
	budget int64
	depth  int
}

func New(file string, prog *ast.Program, out io.Writer, budget int64) *Interpreter {
	if budget <= 0 {
		budget = DefaultBudget
	}
	in := &Interpreter{file: file, prog: prog, out: out, budget: budget, funcs: map[string]*ast.FuncDecl{}}
	for _, fn := range prog.Funcs {
		in.funcs[fn.Name] = fn
	}
	return in
}

func (in *Interpreter) Run() (int64, error) {
	main := in.funcs["main"]
	if main == nil {
		return 0, in.err(token.Pos{}, "missing main function")
	}
	v, err := in.call(main, nil, main.Posn)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func (in *Interpreter) call(fn *ast.FuncDecl, args []int64, pos token.Pos) (int64, error) {
	if in.depth >= MaxCallDepth {
		return 0, in.err(pos, "call stack exceeded %d frames", MaxCallDepth)
	}
	in.depth++
	defer func() { in.depth-- }()
	scope := &env{vars: map[string]int64{}}
	for i, p := range fn.Params {
		scope.vars[p.Name] = args[i]
	}
	if err := in.execBlock(fn.Body, scope); err != nil {
		if rv, ok := err.(*returnValue); ok {
			return rv.value, nil
		}
		return 0, err
	}
	return 0, in.err(fn.Body.Posn, "function %s fell through without return", fn.Name)
}

func (in *Interpreter) tick(pos token.Pos) error {
	if in.budget <= 0 {
		return in.err(pos, "runtime budget exceeded: possible infinite loop")
	}
	in.budget--
	return nil
}

func (in *Interpreter) execBlock(b *ast.Block, parent *env) error {
	scope := &env{parent: parent, vars: map[string]int64{}}
	for _, s := range b.Stmts {
		if err := in.execStmt(s, scope); err != nil {
			return err
		}
	}
	return nil
}

func (in *Interpreter) execStmt(s ast.Stmt, e *env) error {
	if err := in.tick(s.Pos()); err != nil {
		return err
	}
	switch s := s.(type) {
	case *ast.LetStmt:
		v, err := in.eval(s.Init, e)
		if err != nil {
			return err
		}
		if _, exists := e.vars[s.Name]; exists {
			return in.err(s.Posn, "variable %q redeclared", s.Name)
		}
		e.vars[s.Name] = v
	case *ast.AssignStmt:
		if !exists(e, s.Name) {
			return in.err(s.Posn, "undefined variable %q", s.Name)
		}
		v, err := in.eval(s.Value, e)
		if err != nil {
			return err
		}
		assign(e, s.Name, v)
	case *ast.ExprStmt:
		if _, err := in.eval(s.X, e); err != nil {
			return err
		}
	case *ast.IfStmt:
		v, err := in.eval(s.Cond, e)
		if err != nil {
			return err
		}
		if v != 0 {
			return in.execBlock(s.Then, e)
		}
		if s.Else != nil {
			return in.execStmt(s.Else, e)
		}
	case *ast.WhileStmt:
		for {
			if err := in.tick(s.Posn); err != nil {
				return err
			}
			v, err := in.eval(s.Cond, e)
			if err != nil {
				return err
			}
			if v == 0 {
				break
			}
			if err := in.execBlock(s.Body, e); err != nil {
				return err
			}
		}
	case *ast.ReturnStmt:
		v, err := in.eval(s.Value, e)
		if err != nil {
			return err
		}
		return &returnValue{value: v}
	case *ast.BlockStmt:
		return in.execBlock(s.Block, e)
	}
	return nil
}

func assign(e *env, name string, v int64) bool {
	for s := e; s != nil; s = s.parent {
		if _, ok := s.vars[name]; ok {
			s.vars[name] = v
			return true
		}
	}
	return false
}

func exists(e *env, name string) bool {
	for s := e; s != nil; s = s.parent {
		if _, ok := s.vars[name]; ok {
			return true
		}
	}
	return false
}

func lookup(e *env, name string) (int64, bool) {
	for s := e; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v, true
		}
		if s.parent == nil {
			break
		}
	}
	return 0, false
}

func (in *Interpreter) eval(e0 ast.Expr, env0 *env) (int64, error) {
	if err := in.tick(e0.Pos()); err != nil {
		return 0, err
	}
	switch e := e0.(type) {
	case *ast.IntLit:
		return e.Value, nil
	case *ast.BoolLit:
		return btoi(e.Value), nil
	case *ast.Ident:
		v, ok := lookup(env0, e.Name)
		if !ok {
			return 0, in.err(e.Posn, "undefined variable %q", e.Name)
		}
		return v, nil
	case *ast.UnaryExpr:
		v, err := in.eval(e.X, env0)
		if err != nil {
			return 0, err
		}
		if e.Op == token.MINUS {
			if v == -1<<63 {
				return 0, in.err(e.Posn, "integer negation overflows")
			}
			return -v, nil
		}
		return btoi(v == 0), nil
	case *ast.BinaryExpr:
		if e.Op == token.AND || e.Op == token.OR {
			l, err := in.eval(e.L, env0)
			if err != nil {
				return 0, err
			}
			if e.Op == token.AND {
				if l == 0 {
					return 0, nil
				}
			} else if l != 0 {
				return 1, nil
			}
			return in.eval(e.R, env0)
		}
		l, err := in.eval(e.L, env0)
		if err != nil {
			return 0, err
		}
		r, err := in.eval(e.R, env0)
		if err != nil {
			return 0, err
		}
		return binary(e.Op, l, r, e.Posn, in)
	case *ast.CallExpr:
		args := make([]int64, len(e.Args))
		for i, a := range e.Args {
			v, err := in.eval(a, env0)
			if err != nil {
				return 0, err
			}
			args[i] = v
		}
		switch e.Name {
		case "print":
			fmt.Fprintln(in.out, args[0])
			return 0, nil
		case "print_bool":
			fmt.Fprintln(in.out, formatBool(args[0] != 0))
			return 0, nil
		}
		fn := in.funcs[e.Name]
		if fn == nil {
			return 0, in.err(e.Posn, "undefined function %q", e.Name)
		}
		return in.call(fn, args, e.Posn)
	}
	return 0, in.err(e0.Pos(), "unsupported expression")
}

func binary(op token.Kind, l, r int64, pos token.Pos, in *Interpreter) (int64, error) {
	switch op {
	case token.PLUS:
		v, carry := bits.Add64(uint64(l), uint64(r), 0)
		if carry != ((^uint64(l)&^uint64(r)&v | (l64(l) & l64(r) &^ v)) >> 63) {
			return 0, in.err(pos, "integer addition overflows")
		}
		return int64(v), nil
	case token.MINUS:
		v, borrow := bits.Sub64(uint64(l), uint64(r), 0)
		if borrow != (((uint64(l) ^ uint64(r)) & (uint64(l) ^ v)) >> 63) {
			return 0, in.err(pos, "integer subtraction overflows")
		}
		return int64(v), nil
	case token.STAR:
		hi, v := bits.Mul64(uint64(l), uint64(r))
		if hi != uint64(int64(v)>>63) {
			return 0, in.err(pos, "integer multiplication overflows")
		}
		return int64(v), nil
	case token.SLASH:
		if r == 0 {
			return 0, in.err(pos, "division by zero")
		}
		if l == -1<<63 && r == -1 {
			return 0, in.err(pos, "division overflows")
		}
		return l / r, nil
	case token.PERCENT:
		if r == 0 {
			return 0, in.err(pos, "modulo by zero")
		}
		if l == -1<<63 && r == -1 {
			return 0, nil
		}
		return l % r, nil
	case token.EQ:
		return btoi(l == r), nil
	case token.NE:
		return btoi(l != r), nil
	case token.LT:
		return btoi(l < r), nil
	case token.GT:
		return btoi(l > r), nil
	case token.LE:
		return btoi(l <= r), nil
	case token.GE:
		return btoi(l >= r), nil
	}
	return 0, in.err(pos, "unsupported operator")
}

func l64(v int64) uint64 { return uint64(v) }

func formatBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
func btoi(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (in *Interpreter) err(pos token.Pos, format string, args ...any) error {
	return tlerr.At(in.file, pos, "runtime error: %s", fmt.Sprintf(format, args...))
}
