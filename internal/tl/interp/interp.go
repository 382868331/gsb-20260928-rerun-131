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

type returnValue struct{ value value }

func (*returnValue) Error() string { return "return" }

type valueKind int

const (
	valInt valueKind = iota
	valFunc
)

type value struct {
	kind valueKind
	i    int64
	fun  *closure
}

type cell struct{ value value }

type env struct {
	parent *env
	base   int
	vars   map[string]*cell
}

type callable interface {
	params() []*ast.Param
	result() ast.Type
	body() *ast.Block
}

type topFunc struct{ fn *ast.FuncDecl }

func (t *topFunc) params() []*ast.Param { return t.fn.Params }
func (t *topFunc) result() ast.Type     { return t.fn.Result }
func (t *topFunc) body() *ast.Block     { return t.fn.Body }

type litFunc struct{ fn *ast.FuncLit }

func (l *litFunc) params() []*ast.Param { return l.fn.Params }
func (l *litFunc) result() ast.Type     { return l.fn.Result }
func (l *litFunc) body() *ast.Block     { return l.fn.Body }

type closure struct {
	target callable
	env    *env
	name   string
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
	v, err := in.call(&closure{target: &topFunc{fn: main}, name: "main"}, nil, main.Posn)
	if err != nil {
		return 0, err
	}
	return v.i, nil
}

func (in *Interpreter) call(cl *closure, args []value, pos token.Pos) (value, error) {
	if in.depth >= MaxCallDepth {
		return value{}, in.err(pos, "call stack exceeded %d frames", MaxCallDepth)
	}
	in.depth++
	defer func() { in.depth-- }()
	fn := cl.target
	scope := &env{parent: cl.env, vars: map[string]*cell{}}
	for i, p := range fn.params() {
		scope.vars[p.Name] = &cell{value: args[i]}
	}
	if lit, ok := fn.(*litFunc); ok && lit.fn.Name != "" {
		scope.vars[lit.fn.Name] = &cell{value: value{kind: valFunc, fun: cl}}
	}
	if err := in.execBlock(fn.body(), scope); err != nil {
		if rv, ok := err.(*returnValue); ok {
			return rv.value, nil
		}
		return value{}, err
	}
	return value{}, in.err(fn.body().Posn, "function fell through without return")
}

func (in *Interpreter) tick(pos token.Pos) error {
	if in.budget <= 0 {
		return in.err(pos, "runtime budget exceeded: possible infinite loop")
	}
	in.budget--
	return nil
}

func (in *Interpreter) err(pos token.Pos, format string, args ...any) error {
	return tlerr.At(in.file, pos, "runtime error: %s", fmt.Sprintf(format, args...))
}

func (in *Interpreter) execBlock(b *ast.Block, parent *env) error {
	scope := &env{parent: parent, base: b.SlotBase, vars: map[string]*cell{}}
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
		e.vars[s.Name] = &cell{value: v}
	case *ast.AssignStmt:
		cell := findCell(e, s.Name)
		if cell == nil {
			return in.err(s.Posn, "undefined variable %q", s.Name)
		}
		v, err := in.eval(s.Value, e)
		if err != nil {
			return err
		}
		cell.value = v
	case *ast.ExprStmt:
		if _, err := in.eval(s.X, e); err != nil {
			return err
		}
	case *ast.IfStmt:
		v, err := in.eval(s.Cond, e)
		if err != nil {
			return err
		}
		if v.i != 0 {
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
			if v.i == 0 {
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

func findCell(e *env, name string) *cell {
	for s := e; s != nil; s = s.parent {
		if c, ok := s.vars[name]; ok {
			return c
		}
	}
	return nil
}

func (in *Interpreter) eval(e0 ast.Expr, env0 *env) (value, error) {
	if err := in.tick(e0.Pos()); err != nil {
		return value{}, err
	}
	switch e := e0.(type) {
	case *ast.IntLit:
		return value{i: e.Value}, nil
	case *ast.BoolLit:
		return value{i: btoi(e.Value)}, nil
	case *ast.Ident:
		if c := findCell(env0, e.Name); c != nil {
			return c.value, nil
		}
		if fn := in.funcs[e.Name]; fn != nil {
			return value{kind: valFunc, fun: &closure{target: &topFunc{fn: fn}, name: fn.Name}}, nil
		}
		return value{}, in.err(e.Posn, "undefined variable %q", e.Name)
	case *ast.FuncLit:
		cl := &closure{target: &litFunc{fn: e}, env: env0, name: e.Name}
		return value{kind: valFunc, fun: cl}, nil
	case *ast.UnaryExpr:
		v, err := in.eval(e.X, env0)
		if err != nil {
			return value{}, err
		}
		if e.Op == token.MINUS {
			if v.i == -1<<63 {
				return value{}, in.err(e.Posn, "integer negation overflows")
			}
			v.i = -v.i
			return v, nil
		}
		v.i = btoi(v.i == 0)
		return v, nil
	case *ast.BinaryExpr:
		if e.Op == token.AND || e.Op == token.OR {
			l, err := in.eval(e.L, env0)
			if err != nil {
				return value{}, err
			}
			if e.Op == token.AND && l.i == 0 || e.Op == token.OR && l.i != 0 {
				return l, nil
			}
			return in.eval(e.R, env0)
		}
		l, err := in.eval(e.L, env0)
		if err != nil {
			return value{}, err
		}
		r, err := in.eval(e.R, env0)
		if err != nil {
			return value{}, err
		}
		v, err := binary(e.Op, l.i, r.i, e.Posn, in)
		if err != nil {
			return value{}, err
		}
		return value{i: v}, nil
	case *ast.CallExpr:
		args := make([]value, len(e.Args))
		for i, a := range e.Args {
			v, err := in.eval(a, env0)
			if err != nil {
				return value{}, err
			}
			args[i] = v
		}
		if e.Builtin != "" {
			return in.callBuiltin(e.Builtin, args, e.Posn)
		}
		var cl *closure
		if e.Name != "" && !e.Dynamic {
			fn := in.funcs[e.Name]
			if fn == nil {
				return value{}, in.err(e.Posn, "undefined function %q", e.Name)
			}
			cl = &closure{target: &topFunc{fn: fn}, name: fn.Name}
		} else {
			cv, err := in.eval(e.Callee, env0)
			if err != nil {
				return value{}, err
			}
			if cv.kind != valFunc {
				return value{}, in.err(e.Posn, "cannot call non-function value")
			}
			cl = cv.fun
		}
		if len(args) != len(cl.target.params()) {
			return value{}, in.err(e.Posn, "arity violation")
		}
		return in.call(cl, args, e.Posn)
	}
	return value{}, in.err(e0.Pos(), "unsupported expression")
}

func (in *Interpreter) callBuiltin(name string, args []value, pos token.Pos) (value, error) {
	switch name {
	case "print":
		fmt.Fprintln(in.out, args[0].i)
	case "print_bool":
		fmt.Fprintln(in.out, formatBool(args[0].i != 0))
	default:
		return value{}, in.err(pos, "unknown built-in function")
	}
	return value{i: 0}, nil
}

func binary(op token.Kind, l, r int64, pos token.Pos, in *Interpreter) (int64, error) {
	switch op {
	case token.PLUS:
		v, carry := bits.Add64(uint64(l), uint64(r), 0)
		if carry != ((^uint64(l)&^uint64(r)&v | (uint64(l) & uint64(r) &^ v)) >> 63) {
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
