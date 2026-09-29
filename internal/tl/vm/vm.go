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

type frame struct {
	fn     *ir.Function
	ip     int
	locals []int64
	stack  []int64
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
	return m.call(uint32(m.prog.Main), nil, token.Pos{})
}

func (m *VM) call(index uint32, args []int64, callPos token.Pos) (int64, error) {
	if int(index) >= len(m.prog.Funcs) {
		return 0, m.err(callPos, "invalid call target %d", index)
	}
	fn := m.prog.Funcs[index]
	if len(args) != int(fn.Params) {
		return 0, m.err(callPos, "arity violation calling %s", fn.Name)
	}
	if m.depth >= MaxCallDepth {
		return 0, m.err(callPos, "call stack exceeded %d frames", MaxCallDepth)
	}
	m.depth++
	defer func() { m.depth-- }()

	fr := &frame{fn: fn, locals: make([]int64, fn.NumLocals)}
	copy(fr.locals, args)

	for fr.ip < len(fn.Code) {
		if m.budget <= 0 {
			return 0, m.err(m.pos(fr), "runtime budget exceeded: possible infinite loop")
		}
		m.budget--
		ins := fn.Code[fr.ip]
		line := token.Pos{Line: int(ins.Line), Column: 1}
		fr.ip++

		switch ins.Op {
		case ir.OpConst, ir.OpLoad:
			var v int64
			if ins.Op == ir.OpConst {
				if int(ins.Arg) >= len(fn.Consts) {
					return 0, m.bad(line)
				}
				v = fn.Consts[ins.Arg]
			} else {
				if int(ins.Arg) >= len(fr.locals) {
					return 0, m.bad(line)
				}
				v = fr.locals[ins.Arg]
			}
			push(fr, v)
		case ir.OpStore:
			v, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			if int(ins.Arg) >= len(fr.locals) {
				return 0, m.bad(line)
			}
			fr.locals[ins.Arg] = v
		case ir.OpPop:
			if _, err := pop(fr, line, m); err != nil {
				return 0, err
			}
		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod:
			r, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			l, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			v, err := arith(ins.Op, l, r, line, m)
			if err != nil {
				return 0, err
			}
			push(fr, v)
		case ir.OpNeg:
			v, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			if v == -1<<63 {
				return 0, m.err(line, "integer negation overflows")
			}
			push(fr, -v)
		case ir.OpNot:
			v, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			push(fr, btoi(v == 0))
		case ir.OpEq, ir.OpNe, ir.OpLt, ir.OpGt, ir.OpLe, ir.OpGe:
			r, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			l, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			push(fr, compare(ins.Op, l, r))
		case ir.OpJump:
			fr.ip = int(ins.Arg)
		case ir.OpJumpIfFalse, ir.OpJumpIfTrue:
			if len(fr.stack) == 0 {
				return 0, m.bad(line)
			}
			jump := top(fr) == 0
			if ins.Op == ir.OpJumpIfTrue {
				jump = top(fr) != 0
			}
			if jump {
				fr.ip = int(ins.Arg)
			}
		case ir.OpPopJumpIfFalse:
			v, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			if v == 0 {
				fr.ip = int(ins.Arg)
			}
		case ir.OpCall:
			if int(ins.Arg) >= len(m.prog.Funcs) {
				return 0, m.bad(line)
			}
			target := m.prog.Funcs[ins.Arg]
			if int(target.Params) > len(fr.stack) {
				return 0, m.bad(line)
			}
			base := len(fr.stack) - int(target.Params)
			args := append([]int64(nil), fr.stack[base:]...)
			fr.stack = fr.stack[:base]
			v, err := m.call(ins.Arg, args, line)
			if err != nil {
				return 0, err
			}
			push(fr, v)
		case ir.OpCallBuiltin:
			v, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			switch ins.Arg {
			case 0:
				fmt.Fprintln(m.out, v)
			case 1:
				fmt.Fprintln(m.out, formatBool(v != 0))
			default:
				return 0, m.bad(line)
			}
			push(fr, 0)
		case ir.OpReturn:
			v, err := pop(fr, line, m)
			if err != nil {
				return 0, err
			}
			if len(fr.stack) != 0 {
				return 0, m.bad(line)
			}
			return v, nil
		case ir.OpHalt:
			return 0, m.err(line, "execution halted without return")
		default:
			return 0, m.bad(line)
		}
		if fr.ip < 0 || fr.ip > len(fn.Code) {
			return 0, m.err(line, "jump out of function %s", fn.Name)
		}
	}
	return 0, m.err(token.Pos{Line: 1, Column: 1}, "function %s fell through without return", fn.Name)
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

func push(f *frame, v int64) { f.stack = append(f.stack, v) }
func top(f *frame) int64     { return f.stack[len(f.stack)-1] }

func pop(f *frame, pos token.Pos, m *VM) (int64, error) {
	if len(f.stack) == 0 {
		return 0, m.bad(pos)
	}
	v := f.stack[len(f.stack)-1]
	f.stack = f.stack[:len(f.stack)-1]
	return v, nil
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
