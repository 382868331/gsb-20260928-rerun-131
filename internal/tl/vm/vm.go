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

type valueKind byte

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

type environment struct {
	parent *environment
	base   int
	slots  map[int]*cell
}

type closure struct {
	fn  *ir.Function
	env *environment
}

type frame struct {
	fn    *ir.Function
	ip    int
	env   *environment
	stack []value
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

func (m *VM) Run() (value, error) {
	return m.call(uint32(m.prog.Main), nil, token.Pos{})
}

func (m *VM) RunInt() (int64, error) {
	v, err := m.Run()
	if err != nil {
		return 0, err
	}
	if v.kind != valInt {
		return 0, m.err(token.Pos{}, "main must return int")
	}
	return v.i, nil
}

func (m *VM) call(index uint32, args []value, callPos token.Pos) (value, error) {
	if int(index) >= len(m.prog.Funcs) {
		return value{}, m.err(callPos, "invalid call target %d", index)
	}
	return m.callClosure(&closure{fn: m.prog.Funcs[index]}, args, callPos)
}

func (m *VM) callClosure(cl *closure, args []value, callPos token.Pos) (value, error) {
	fn := cl.fn
	if len(args) != int(fn.Params) {
		return value{}, m.err(callPos, "arity violation calling %s", fn.Name)
	}
	if m.depth >= MaxCallDepth {
		return value{}, m.err(callPos, "call stack exceeded %d frames", MaxCallDepth)
	}
	m.depth++
	defer func() { m.depth-- }()

	fr := &frame{fn: fn, env: &environment{parent: cl.env, base: int(fn.RootBase), slots: map[int]*cell{}}}
	for i, a := range args {
		m.define(fr, int(fn.RootBase)+i, a)
	}
	if fn.SelfSlot >= 0 {
		m.define(fr, int(fn.SelfSlot), value{kind: valFunc, fun: cl})
	}

	for fr.ip < len(fn.Code) {
		if m.budget <= 0 {
			return value{}, m.err(m.pos(fr), "runtime budget exceeded: possible infinite loop")
		}
		m.budget--
		ins := fn.Code[fr.ip]
		line := token.Pos{Line: int(ins.Line), Column: 1}
		fr.ip++

		switch ins.Op {
		case ir.OpConst:
			if int(ins.Arg) >= len(fn.Consts) {
				return value{}, m.bad(line)
			}
			push(fr, value{i: fn.Consts[ins.Arg]})
		case ir.OpLoad:
			v, ok := m.lookup(fr, int(ins.Arg), line)
			if !ok {
				return value{}, m.bad(line)
			}
			push(fr, v)
		case ir.OpStore:
			v, err := pop(fr, line, m)
			if err != nil {
				return value{}, err
			}
			if !m.store(fr, int(ins.Arg), v, line) {
				return value{}, m.bad(line)
			}
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
			push(fr, value{i: v})
		case ir.OpNeg:
			v, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			if v == -1<<63 {
				return value{}, m.err(line, "integer negation overflows")
			}
			push(fr, value{i: -v})
		case ir.OpNot:
			v, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			push(fr, value{i: btoi(v == 0)})
		case ir.OpEq, ir.OpNe, ir.OpLt, ir.OpGt, ir.OpLe, ir.OpGe:
			r, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			l, err := popInt(fr, line, m)
			if err != nil {
				return value{}, err
			}
			push(fr, value{i: compare(ins.Op, l, r)})
		case ir.OpJump:
			fr.ip = int(ins.Arg)
		case ir.OpJumpIfFalse, ir.OpJumpIfTrue:
			if len(fr.stack) == 0 {
				return value{}, m.bad(line)
			}
			v, err := topInt(fr, line)
			if err != nil {
				return value{}, m.bad(line)
			}
			jump := v == 0
			if ins.Op == ir.OpJumpIfTrue {
				jump = v != 0
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
		case ir.OpClosure:
			if int(ins.Arg) >= len(m.prog.Funcs) {
				return value{}, m.bad(line)
			}
			push(fr, value{kind: valFunc, fun: &closure{fn: m.prog.Funcs[ins.Arg], env: fr.env}})
		case ir.OpEnterScope:
			fr.env = &environment{parent: fr.env, base: int(ins.Arg), slots: map[int]*cell{}}
		case ir.OpLeaveScope:
			if fr.env.parent == nil {
				return value{}, m.bad(line)
			}
			fr.env = fr.env.parent
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
			v, err := m.call(ins.Arg, args, line)
			if err != nil {
				return value{}, err
			}
			push(fr, v)
		case ir.OpCallValue:
			arity := int(ins.Arg)
			if arity+1 > len(fr.stack) {
				return value{}, m.bad(line)
			}
			base := len(fr.stack) - arity - 1
			callee := fr.stack[len(fr.stack)-1]
			if callee.kind != valFunc {
				return value{}, m.err(line, "cannot call non-function value")
			}
			args := append([]value(nil), fr.stack[base:base+arity]...)
			fr.stack = fr.stack[:base]
			v, err := m.callClosure(callee.fun, args, line)
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
			push(fr, value{i: 0})
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

func (m *VM) lookup(fr *frame, slot int, pos token.Pos) (value, bool) {
	if slot < 0 {
		return value{}, false
	}
	for e := fr.env; e != nil; e = e.parent {
		if c, ok := e.slots[slot]; ok {
			return c.value, true
		}
	}
	return value{}, false
}

func (m *VM) store(fr *frame, slot int, v value, pos token.Pos) bool {
	if slot < 0 {
		return false
	}
	for e := fr.env; e != nil; e = e.parent {
		if c, ok := e.slots[slot]; ok {
			c.value = v
			return true
		}
	}
	fr.env.slots[slot] = &cell{value: v}
	return true
}

func (m *VM) define(fr *frame, slot int, v value) bool {
	if slot < 0 {
		return false
	}
	fr.env.slots[slot] = &cell{value: v}
	return true
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

func topInt(f *frame, pos token.Pos) (int64, error) {
	v := top(f)
	if v.kind != valInt {
		return 0, fmt.Errorf("function used as int at %v", pos)
	}
	return v.i, nil
}

func pop(f *frame, pos token.Pos, m *VM) (value, error) {
	if len(f.stack) == 0 {
		return value{}, m.bad(pos)
	}
	v := f.stack[len(f.stack)-1]
	f.stack = f.stack[:len(f.stack)-1]
	return v, nil
}

func popInt(f *frame, pos token.Pos, m *VM) (int64, error) {
	v, err := pop(f, pos, m)
	if err != nil {
		return 0, err
	}
	if v.kind != valInt {
		return 0, m.err(pos, "function value used where int was expected")
	}
	return v.i, nil
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
