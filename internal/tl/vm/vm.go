package vm

import (
	"fmt"
	"io"
	"math/bits"

	"tl/internal/tl/ir"
	"tl/internal/tl/tlerr"
	"tl/internal/tl/token"
)

const DefaultBudget = 10_000_000
const MaxCallDepth = 1000

// cell is the heap storage behind a captured variable. Closures created
// in the same scope share the cell; separate calls create separate cells.
type cell struct{ v value }

// closure pairs a code body with the captured cells of its definition
// site (lexical scope, not the dynamic call stack).
type closure struct {
	fn   uint32
	caps []*cell
}

// value is a tagged runtime value: a plain integer/boolean (clo == nil
// && cl == nil), a heap cell, or a closure.
type value struct {
	n  int64
	cl *cell
	co *closure
}

func intVal(n int64) value    { return value{n: n} }
func cellVal(c *cell) value   { return value{cl: c} }
func cloVal(c *closure) value { return value{co: c} }

type frame struct {
	fn     *ir.Function
	ip     int
	locals []value
	caps   []*cell
	stack  []value
}

type VM struct {
	prog   *ir.Program
	file   string
	out    io.Writer
	budget int64
	depth  int
}

func New(prog *ir.Program, file string, out io.Writer, budget int64) *VM {
	if budget <= 0 {
		budget = DefaultBudget
	}
	return &VM{prog: prog, file: file, out: out, budget: budget}
}

func (m *VM) Run() (int64, error) {
	v, err := m.call(uint32(m.prog.Main), nil, nil, token.Pos{})
	if err != nil {
		return 0, err
	}
	if v.cl != nil || v.co != nil {
		return 0, m.err(token.Pos{}, "main must return an int")
	}
	return v.n, nil
}

func (m *VM) call(index uint32, args []value, caps []*cell, callPos token.Pos) (value, error) {
	if int(index) >= len(m.prog.Funcs) {
		return value{}, m.err(callPos, "invalid call target %d", index)
	}
	fn := m.prog.Funcs[index]
	if len(args) != int(fn.Params) {
		return value{}, m.err(callPos, "arity violation calling %s", fn.Name)
	}
	if len(caps) != int(fn.NumCaps) {
		return value{}, m.err(callPos, "capture count violation calling %s", fn.Name)
	}
	if m.depth >= MaxCallDepth {
		return value{}, m.err(callPos, "call stack exceeded %d frames", MaxCallDepth)
	}
	m.depth++
	defer func() { m.depth-- }()

	fr := &frame{fn: fn, locals: make([]value, fn.NumLocals), caps: caps}
	copy(fr.locals, args)

	for fr.ip < len(fn.Code) {
		if m.budget <= 0 {
			return value{}, m.err(m.pos(fr), "runtime budget exceeded: possible infinite loop")
		}
		m.budget--
		ins := fn.Code[fr.ip]
		line := token.Pos{Line: int(ins.Line), Column: 1}
		fr.ip++

		switch ins.Op {
		case ir.OpConst, ir.OpLoad:
			var v value
			if ins.Op == ir.OpConst {
				if int(ins.Arg) >= len(fn.Consts) {
					return value{}, m.bad(line)
				}
				v = intVal(fn.Consts[ins.Arg])
			} else {
				if int(ins.Arg) >= len(fr.locals) {
					return value{}, m.bad(line)
				}
				v = fr.locals[ins.Arg]
			}
			push(fr, v)
		case ir.OpStore:
			v, err := pop(fr, line, m)
			if err != nil {
				return value{}, err
			}
			if int(ins.Arg) >= len(fr.locals) {
				return value{}, m.bad(line)
			}
			fr.locals[ins.Arg] = v
		case ir.OpPop:
			if _, err := pop(fr, line, m); err != nil {
				return value{}, err
			}
		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod:
			r, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			l, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			v, err := arith(ins.Op, l, r, line, m)
			if err != nil {
				return value{}, err
			}
			push(fr, intVal(v))
		case ir.OpNeg:
			v, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			if v == -1<<63 {
				return value{}, m.err(line, "integer negation overflows")
			}
			push(fr, intVal(-v))
		case ir.OpNot:
			v, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			push(fr, intVal(btoi(v == 0)))
		case ir.OpEq, ir.OpNe, ir.OpLt, ir.OpGt, ir.OpLe, ir.OpGe:
			r, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			l, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			push(fr, intVal(compare(ins.Op, l, r)))
		case ir.OpJump:
			fr.ip = int(ins.Arg)
		case ir.OpJumpIfFalse, ir.OpJumpIfTrue:
			if len(fr.stack) == 0 {
				return value{}, m.bad(line)
			}
			t := top(fr)
			if t.cl != nil || t.co != nil {
				return value{}, m.bad(line)
			}
			jump := t.n == 0
			if ins.Op == ir.OpJumpIfTrue {
				jump = t.n != 0
			}
			if jump {
				fr.ip = int(ins.Arg)
			}
		case ir.OpPopJumpIfFalse:
			v, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			if v == 0 {
				fr.ip = int(ins.Arg)
			}
		case ir.OpCall:
			if int(ins.Arg) >= len(m.prog.Funcs) {
				return value{}, m.bad(line)
			}
			target := m.prog.Funcs[ins.Arg]
			if int(target.Params) > len(fr.stack) {
				return value{}, m.bad(line)
			}
			base := len(fr.stack) - int(target.Params)
			args := append([]value(nil), fr.stack[base:]...)
			fr.stack = fr.stack[:base]
			v, err := m.call(ins.Arg, args, nil, line)
			if err != nil {
				return value{}, err
			}
			push(fr, v)
		case ir.OpCallBuiltin:
			v, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			switch ins.Arg {
			case 0:
				fmt.Fprintln(m.out, v)
			case 1:
				fmt.Fprintln(m.out, formatBool(v != 0))
			default:
				return value{}, m.bad(line)
			}
			push(fr, intVal(0))
		case ir.OpWrapCell:
			v, err := pop(fr, line, m)
			if err != nil {
				return value{}, err
			}
			if v.cl != nil || v.co != nil {
				return value{}, m.bad(line)
			}
			push(fr, cellVal(&cell{v: v}))
		case ir.OpNewCell:
			push(fr, cellVal(&cell{v: intVal(0)}))
		case ir.OpCellLoad:
			c, err := localCell(fr, ins.Arg, line, m)
			if err != nil {
				return value{}, err
			}
			push(fr, c.v)
		case ir.OpCellStore:
			c, err := localCell(fr, ins.Arg, line, m)
			if err != nil {
				return value{}, err
			}
			v, err := pop(fr, line, m)
			if err != nil {
				return value{}, err
			}
			c.v = v
		case ir.OpCapLoad:
			c, err := frameCap(fr, ins.Arg, line, m)
			if err != nil {
				return value{}, err
			}
			push(fr, c.v)
		case ir.OpCapStore:
			c, err := frameCap(fr, ins.Arg, line, m)
			if err != nil {
				return value{}, err
			}
			v, err := pop(fr, line, m)
			if err != nil {
				return value{}, err
			}
			c.v = v
		case ir.OpCapCell:
			c, err := frameCap(fr, ins.Arg, line, m)
			if err != nil {
				return value{}, err
			}
			push(fr, cellVal(c))
		case ir.OpMakeClosure:
			if int(ins.Arg) >= len(m.prog.Funcs) {
				return value{}, m.bad(line)
			}
			target := m.prog.Funcs[ins.Arg]
			n := int(target.NumCaps)
			if n > len(fr.stack) {
				return value{}, m.bad(line)
			}
			base := len(fr.stack) - n
			caps := make([]*cell, n)
			for i, v := range fr.stack[base:] {
				if v.cl == nil {
					return value{}, m.bad(line)
				}
				caps[i] = v.cl
			}
			fr.stack = fr.stack[:base]
			push(fr, cloVal(&closure{fn: ins.Arg, caps: caps}))
		case ir.OpCallValue:
			argc := int(ins.Arg)
			if argc+1 > len(fr.stack) {
				return value{}, m.bad(line)
			}
			base := len(fr.stack) - argc
			args := append([]value(nil), fr.stack[base:]...)
			callee := fr.stack[base-1]
			if callee.co == nil {
				return value{}, m.bad(line)
			}
			fr.stack = fr.stack[:base-1]
			v, err := m.call(callee.co.fn, args, callee.co.caps, line)
			if err != nil {
				return value{}, err
			}
			push(fr, v)
		case ir.OpReturn:
			v, err := pop(fr, line, m)
			if err != nil {
				return value{}, err
			}
			if len(fr.stack) != 0 {
				return value{}, m.bad(line)
			}
			return v, nil
		case ir.OpHalt:
			return value{}, m.err(line, "execution halted without return")
		default:
			return value{}, m.bad(line)
		}
		if fr.ip < 0 || fr.ip > len(fn.Code) {
			return value{}, m.err(line, "jump out of function %s", fn.Name)
		}
	}
	return value{}, m.err(token.Pos{Line: 1, Column: 1}, "function %s fell through without return", fn.Name)
}

func arith(op ir.Op, l, r int64, pos token.Pos, m *VM) (int64, error) {
	switch op {
	case ir.OpAdd:
		v, carry := bits.Add64(uint64(l), uint64(r), 0)
		if carry != overflowAdd(l, r, v) {
			return 0, m.err(pos, "integer addition overflows")
		}
		return int64(v), nil
	case ir.OpSub:
		v, borrow := bits.Sub64(uint64(l), uint64(r), 0)
		if borrow != overflowSub(l, r, v) {
			return 0, m.err(pos, "integer subtraction overflows")
		}
		return int64(v), nil
	case ir.OpMul:
		hi, v := bits.Mul64(uint64(l), uint64(r))
		if hi != uint64(int64(v)>>63) {
			return 0, m.err(pos, "integer multiplication overflows")
		}
		return int64(v), nil
	case ir.OpDiv:
		if r == 0 {
			return 0, m.err(pos, "division by zero")
		}
		if l == -1<<63 && r == -1 {
			return 0, m.err(pos, "division overflows")
		}
		return l / r, nil
	case ir.OpMod:
		if r == 0 {
			return 0, m.err(pos, "modulo by zero")
		}
		if l == -1<<63 && r == -1 {
			return 0, nil
		}
		return l % r, nil
	}
	return 0, m.bad(pos)
}

func overflowAdd(a int64, b int64, r uint64) uint64 {
	if ^(uint64(a)^uint64(b))&(uint64(a)^r)&(1<<63) != 0 {
		return 1
	}
	return 0
}
func overflowSub(a int64, b int64, r uint64) uint64 {
	if (uint64(a)^uint64(b))&(uint64(a)^r)&(1<<63) != 0 {
		return 1
	}
	return 0
}

func compare(op ir.Op, l, r int64) int64 {
	switch op {
	case ir.OpEq:
		return btoi(l == r)
	case ir.OpNe:
		return btoi(l != r)
	case ir.OpLt:
		return btoi(l < r)
	case ir.OpGt:
		return btoi(l > r)
	case ir.OpLe:
		return btoi(l <= r)
	default:
		return btoi(l >= r)
	}
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

func push(f *frame, v value) { f.stack = append(f.stack, v) }
func top(f *frame) value     { return f.stack[len(f.stack)-1] }

func pop(f *frame, pos token.Pos, m *VM) (value, error) {
	if len(f.stack) == 0 {
		return value{}, m.bad(pos)
	}
	v := f.stack[len(f.stack)-1]
	f.stack = f.stack[:len(f.stack)-1]
	return v, nil
}

// popInt pops a value that must be a plain integer; anything else means
// the IR was not produced by this toolchain's codegen.
func popInt(f *frame, pos token.Pos, m *VM) (int64, error) {
	v, err := pop(f, pos, m)
	if err != nil {
		return 0, err
	}
	if v.cl != nil || v.co != nil {
		return 0, m.bad(pos)
	}
	return v.n, nil
}

func localCell(f *frame, slot uint32, pos token.Pos, m *VM) (*cell, error) {
	if int(slot) >= len(f.locals) {
		return nil, m.bad(pos)
	}
	c := f.locals[slot].cl
	if c == nil {
		return nil, m.bad(pos)
	}
	return c, nil
}

func frameCap(f *frame, idx uint32, pos token.Pos, m *VM) (*cell, error) {
	if int(idx) >= len(f.caps) {
		return nil, m.bad(pos)
	}
	return f.caps[idx], nil
}

func (m *VM) pos(f *frame) token.Pos {
	if f.ip <= 0 || f.ip > len(f.fn.Code) {
		return token.Pos{}
	}
	return token.Pos{Line: int(f.fn.Code[f.ip-1].Line), Column: 1}
}

func (m *VM) err(pos token.Pos, format string, args ...any) error {
	return tlerr.At(m.file, pos, "runtime error: %s", fmt.Sprintf(format, args...))
}

func (m *VM) bad(pos token.Pos) error {
	return m.err(pos, "malformed IR; refusing to continue execution")
}
