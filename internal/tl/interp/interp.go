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

// value is a runtime value: either a plain integer/boolean (clo == nil)
// or a function closure.
type value struct {
	n   int64
	clo *closure
}

// closure pairs a function body with the lexical environment that was
// live when the function expression was evaluated.
type closure struct {
	decl *ast.FuncExpr // anonymous function, or
	fn   *ast.FuncDecl // top-level function used as a value
	env  *env          // captured lexical environment (nil for fn)
}

// cell is the storage behind a variable; closures sharing a variable
// share its cell, so writes through one are visible in the others.
type cell struct{ v value }

type returnValue struct{ value value }

func (*returnValue) Error() string { return "return" }

type env struct {
	parent *env
	vars   map[string]*cell
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
	if v.clo != nil {
		return 0, in.err(main.Posn, "main must return an int")
	}
	return v.n, nil
}

func (in *Interpreter) call(fn *ast.FuncDecl, args []value, pos token.Pos) (value, error) {
	if in.depth >= MaxCallDepth {
		return value{}, in.err(pos, "call stack exceeded %d frames", MaxCallDepth)
	}
	in.depth++
	defer func() { in.depth-- }()
	scope := &env{vars: map[string]*cell{}}
	for i, p := range fn.Params {
		scope.vars[p.Name] = &cell{v: args[i]}
	}
	if err := in.execBlock(fn.Body, scope); err != nil {
		if rv, ok := err.(*returnValue); ok {
			return rv.value, nil
		}
		return value{}, err
	}
	return value{}, in.err(fn.Body.Posn, "function %s fell through without return", fn.Name)
}

// callClosure invokes a lambda with its captured lexical environment;
// the dynamic caller's variables stay invisible to the body.
func (in *Interpreter) callClosure(c *closure, args []value, pos token.Pos) (value, error) {
	if c.fn != nil {
		return in.call(c.fn, args, pos)
	}
	if in.depth >= MaxCallDepth {
		return value{}, in.err(pos, "call stack exceeded %d frames", MaxCallDepth)
	}
	in.depth++
	defer func() { in.depth-- }()
	scope := &env{parent: c.env, vars: map[string]*cell{}}
	for i, p := range c.decl.Params {
		scope.vars[p.Name] = &cell{v: args[i]}
	}
	if err := in.execBlock(c.decl.Body, scope); err != nil {
		if rv, ok := err.(*returnValue); ok {
			return rv.value, nil
		}
		return value{}, err
	}
	return value{}, in.err(c.decl.Body.Posn, "anonymous function fell through without return")
}

func (in *Interpreter) tick(pos token.Pos) error {
	if in.budget <= 0 {
		return in.err(pos, "runtime budget exceeded: possible infinite loop")
	}
	in.budget--
	return nil
}

func (in *Interpreter) execBlock(b *ast.Block, parent *env) error {
	scope := &env{parent: parent, vars: map[string]*cell{}}
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
		if _, isFunc := s.Init.(*ast.FuncExpr); isFunc {
			// Allocate the cell first so a lambda can capture its own
			// variable for recursion.
			if _, exists := e.vars[s.Name]; exists {
				return in.err(s.Posn, "variable %q redeclared", s.Name)
			}
			c := &cell{}
			e.vars[s.Name] = c
			v, err := in.eval(s.Init, e)
			if err != nil {
				return err
			}
			c.v = v
			return nil
		}
		v, err := in.eval(s.Init, e)
		if err != nil {
			return err
		}
		if _, exists := e.vars[s.Name]; exists {
			return in.err(s.Posn, "variable %q redeclared", s.Name)
		}
		e.vars[s.Name] = &cell{v: v}
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
		if v.n != 0 {
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
			if v.n == 0 {
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

func assign(e *env, name string, v value) bool {
	for s := e; s != nil; s = s.parent {
		if c, ok := s.vars[name]; ok {
			c.v = v
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

func lookup(e *env, name string) (value, bool) {
	for s := e; s != nil; s = s.parent {
		if c, ok := s.vars[name]; ok {
			return c.v, true
		}
		if s.parent == nil {
			break
		}
	}
	return value{}, false
}

func (in *Interpreter) eval(e0 ast.Expr, env0 *env) (value, error) {
	if err := in.tick(e0.Pos()); err != nil {
		return value{}, err
	}
	switch e := e0.(type) {
	case *ast.IntLit:
		return value{n: e.Value}, nil
	case *ast.BoolLit:
		return value{n: btoi(e.Value)}, nil
	case *ast.Ident:
		v, ok := lookup(env0, e.Name)
		if ok {
			return v, nil
		}
		if fn := in.funcs[e.Name]; fn != nil {
			return value{clo: &closure{fn: fn}}, nil
		}
		return value{}, in.err(e.Posn, "undefined variable %q", e.Name)
	case *ast.UnaryExpr:
		v, err := in.eval(e.X, env0)
		if err != nil {
			return value{}, err
		}
		if e.Op == token.MINUS {
			if v.n == -1<<63 {
				return value{}, in.err(e.Posn, "integer negation overflows")
			}
			return value{n: -v.n}, nil
		}
		return value{n: btoi(v.n == 0)}, nil
	case *ast.BinaryExpr:
		if e.Op == token.AND || e.Op == token.OR {
			l, err := in.eval(e.L, env0)
			if err != nil {
				return value{}, err
			}
			if e.Op == token.AND {
				if l.n == 0 {
					return value{}, nil
				}
			} else if l.n != 0 {
				return value{n: 1}, nil
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
		v, err := binary(e.Op, l.n, r.n, e.Posn, in)
		if err != nil {
			return value{}, err
		}
		return value{n: v}, nil
	case *ast.CallExpr:
		// A variable in scope shadows the function namespace.
		if cv, ok := lookup(env0, e.Name); ok {
			if cv.clo == nil {
				return value{}, in.err(e.Posn, "%q is not a function", e.Name)
			}
			args, err := in.evalArgs(e.Args, env0)
			if err != nil {
				return value{}, err
			}
			return in.callClosure(cv.clo, args, e.Posn)
		}
		args, err := in.evalArgs(e.Args, env0)
		if err != nil {
			return value{}, err
		}
		switch e.Name {
		case "print":
			fmt.Fprintln(in.out, args[0].n)
			return value{}, nil
		case "print_bool":
			fmt.Fprintln(in.out, formatBool(args[0].n != 0))
			return value{}, nil
		}
		fn := in.funcs[e.Name]
		if fn == nil {
			return value{}, in.err(e.Posn, "undefined function %q", e.Name)
		}
		return in.call(fn, args, e.Posn)
	case *ast.CallValueExpr:
		cv, err := in.eval(e.Callee, env0)
		if err != nil {
			return value{}, err
		}
		if cv.clo == nil {
			return value{}, in.err(e.Posn, "cannot call a non-function value")
		}
		args, err := in.evalArgs(e.Args, env0)
		if err != nil {
			return value{}, err
		}
		return in.callClosure(cv.clo, args, e.Posn)
	case *ast.FuncExpr:
		return value{clo: &closure{decl: e, env: env0}}, nil
	}
	return value{}, in.err(e0.Pos(), "unsupported expression")
}

func (in *Interpreter) evalArgs(exprs []ast.Expr, env0 *env) ([]value, error) {
	args := make([]value, len(exprs))
	for i, a := range exprs {
		v, err := in.eval(a, env0)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	return args, nil
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
