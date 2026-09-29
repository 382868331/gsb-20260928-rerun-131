package tl

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
)

const MaxCallDepth = 1000

type Value struct {
	Type Type
	Int  int64
	Bool bool
}

type frame struct {
	fn     *IRFunction
	ip     int
	locals []Value
	stack  []Value
}

type VM struct {
	program *IRProgram
	budget  int64
	out     io.Writer
}

func NewVM(program *IRProgram, budget int64, out io.Writer) (*VM, error) {
	if err := Validate(program); err != nil {
		return nil, err
	}
	if budget <= 0 {
		return nil, fmt.Errorf("budget must be positive")
	}
	return &VM{program: program, budget: budget, out: out}, nil
}

func DecodeIR(data []byte) (*IRProgram, error) {
	var program IRProgram
	if err := json.Unmarshal(data, &program); err != nil {
		return nil, fmt.Errorf("invalid intermediate file: %w", err)
	}
	return &program, nil
}

func (vm *VM) Run() (int64, error) {
	mainIndex := -1
	for i := range vm.program.Functions {
		if vm.program.Functions[i].Name == "main" {
			mainIndex = i
		}
	}
	if mainIndex < 0 {
		return 0, fmt.Errorf("missing main")
	}
	main := &vm.program.Functions[mainIndex]
	calls := []*frame{{fn: main, locals: make([]Value, main.LocalSize)}}
	var result Value
	for {
		current := calls[len(calls)-1]
		if current.ip >= len(current.fn.Code) {
			return 0, &RuntimeError{Pos: Position{}, Msg: fmt.Sprintf("function %q fell off the end", current.fn.Name)}
		}
		ins := current.fn.Code[current.ip]
		current.ip++
		vm.budget--
		if vm.budget < 0 {
			return 0, &RuntimeError{Pos: ins.Pos, Msg: "run budget exhausted"}
		}
		switch ins.Op {
		case OpConstInt:
			current.push(Value{Type: IntType, Int: ins.Arg})
		case OpConstBool:
			current.push(Value{Type: BoolType, Bool: ins.Arg == 1})
		case OpLoadLocal:
			current.push(current.locals[int(ins.Arg)])
		case OpStoreLocal:
			current.locals[int(ins.Arg)] = current.pop()
		case OpPop:
			if _, err := current.popChecked(); err != nil {
				return 0, runtimeAt(ins.Pos, err)
			}
		case OpNegate:
			value, err := current.popChecked()
			if err != nil {
				return 0, runtimeAt(ins.Pos, err)
			}
			if value.Type != IntType {
				return 0, &RuntimeError{Pos: ins.Pos, Msg: "unary negation requires int"}
			}
			current.push(Value{Type: IntType, Int: -value.Int})
		case OpNot:
			value, err := current.popChecked()
			if err != nil {
				return 0, runtimeAt(ins.Pos, err)
			}
			if value.Type != BoolType {
				return 0, &RuntimeError{Pos: ins.Pos, Msg: "logical negation requires bool"}
			}
			current.push(Value{Type: BoolType, Bool: !value.Bool})
		case OpReturn:
			value, err := current.popChecked()
			if err != nil {
				return 0, runtimeAt(ins.Pos, err)
			}
			if current.fn.Name == "main" {
				result = value
				return result.Int, nil
			}
			calls = calls[:len(calls)-1]
			calls[len(calls)-1].push(value)
		case OpPrint:
			value, err := current.popChecked()
			if err != nil {
				return 0, runtimeAt(ins.Pos, err)
			}
			if value.Type == IntType {
				_, _ = fmt.Fprintln(vm.out, value.Int)
			} else {
				_, _ = fmt.Fprintln(vm.out, value.Bool)
			}
		case OpJump:
			current.ip = int(ins.Arg)
		case OpJumpIfFalse:
			value, err := current.popChecked()
			if err != nil {
				return 0, runtimeAt(ins.Pos, err)
			}
			if value.Type != BoolType {
				return 0, &RuntimeError{Pos: ins.Pos, Msg: "branch condition must be bool"}
			}
			if !value.Bool {
				current.ip = int(ins.Arg)
			}
		case OpCall:
			if ins.Arg < 0 || int(ins.Arg) >= len(vm.program.Functions) {
				return 0, &RuntimeError{Pos: ins.Pos, Msg: "invalid call target"}
			}
			callee := &vm.program.Functions[int(ins.Arg)]
			if len(calls) >= MaxCallDepth {
				return 0, &RuntimeError{Pos: ins.Pos, Msg: "maximum call depth exceeded"}
			}
			if len(callee.Params) > len(current.stack) {
				return 0, &RuntimeError{Pos: ins.Pos, Msg: "not enough arguments"}
			}
			args := make([]Value, len(callee.Params))
			for i := len(args) - 1; i >= 0; i-- {
				arg, err := current.popChecked()
				if err != nil {
					return 0, runtimeAt(ins.Pos, err)
				}
				args[i] = arg
			}
			calls = append(calls, &frame{fn: callee, locals: make([]Value, callee.LocalSize)})
			copy(calls[len(calls)-1].locals, args)
		default:
			if err := vm.executeBinary(current, ins.Op, ins.Pos); err != nil {
				return 0, err
			}
		}
	}
}

func (f *frame) push(value Value) { f.stack = append(f.stack, value) }
func (f *frame) pop() Value       { v, _ := f.popChecked(); return v }
func (f *frame) popChecked() (Value, error) {
	if len(f.stack) == 0 {
		return Value{}, fmt.Errorf("empty operand stack")
	}
	v := f.stack[len(f.stack)-1]
	f.stack = f.stack[:len(f.stack)-1]
	return v, nil
}

func (vm *VM) executeBinary(f *frame, op string, pos Position) error {
	right, err := f.popChecked()
	if err != nil {
		return runtimeAt(pos, err)
	}
	left, err := f.popChecked()
	if err != nil {
		return runtimeAt(pos, err)
	}
	switch op {
	case OpAdd, OpSubtract, OpMultiply, OpDivide, OpLess, OpLessEqual, OpGreater, OpGreaterEqual:
		if left.Type != IntType || right.Type != IntType {
			return &RuntimeError{Pos: pos, Msg: "integer operation received non-integer value"}
		}
		var value int64
		switch op {
		case OpAdd:
			value = left.Int + right.Int
		case OpSubtract:
			value = left.Int - right.Int
		case OpMultiply:
			value = left.Int * right.Int
		case OpDivide:
			if right.Int == 0 {
				return &RuntimeError{Pos: pos, Msg: "integer division by zero"}
			}
			if left.Int == math.MinInt64 && right.Int == -1 {
				value = math.MinInt64
			} else {
				value = left.Int / right.Int
			}
		case OpLess:
			f.push(Value{Type: BoolType, Bool: left.Int < right.Int})
			return nil
		case OpLessEqual:
			f.push(Value{Type: BoolType, Bool: left.Int <= right.Int})
			return nil
		case OpGreater:
			f.push(Value{Type: BoolType, Bool: left.Int > right.Int})
			return nil
		case OpGreaterEqual:
			f.push(Value{Type: BoolType, Bool: left.Int >= right.Int})
			return nil
		}
		f.push(Value{Type: IntType, Int: value})
	case OpEqual, OpNotEqual:
		if left.Type != right.Type {
			return &RuntimeError{Pos: pos, Msg: "comparing mismatched values"}
		}
		var equal bool
		if left.Type == IntType {
			equal = left.Int == right.Int
		} else {
			equal = left.Bool == right.Bool
		}
		if op == OpNotEqual {
			equal = !equal
		}
		f.push(Value{Type: BoolType, Bool: equal})
	case OpAnd, OpOr:
		if left.Type != BoolType || right.Type != BoolType {
			return &RuntimeError{Pos: pos, Msg: "logical operation requires bool values"}
		}
		value := left.Bool && right.Bool
		if op == OpOr {
			value = left.Bool || right.Bool
		}
		f.push(Value{Type: BoolType, Bool: value})
	default:
		return &RuntimeError{Pos: pos, Msg: "unknown opcode " + op}
	}
	return nil
}

func runtimeAt(pos Position, err error) error {
	return &RuntimeError{Pos: pos, Msg: err.Error()}
}
