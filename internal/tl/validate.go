package tl

import "fmt"

func Validate(program *IRProgram) error {
	if program.Version != IRVersion {
		return fmt.Errorf("unsupported IR version %d", program.Version)
	}
	if len(program.Functions) == 0 {
		return fmt.Errorf("IR contains no functions")
	}
	hasMain := false
	seen := map[string]bool{}
	for fi := range program.Functions {
		fn := &program.Functions[fi]
		if fn.Name == "" {
			return fmt.Errorf("function %d has no name", fi)
		}
		if seen[fn.Name] {
			return fmt.Errorf("duplicate IR function %q", fn.Name)
		}
		seen[fn.Name] = true
		if fn.Name == "main" {
			hasMain = true
			if len(fn.Params) != 0 || fn.Result != IntType {
				return fmt.Errorf("invalid main signature")
			}
		}
		if fn.LocalSize < 0 || fn.LocalSize > 100000 {
			return fmt.Errorf("invalid local size in %q", fn.Name)
		}
		if len(fn.Code) == 0 {
			return fmt.Errorf("function %q has empty code", fn.Name)
		}
		for ip, ins := range fn.Code {
			if !validOpcodes[ins.Op] {
				return fmt.Errorf("%s:%d: unknown opcode %q", fn.Name, ip, ins.Op)
			}
			if err := validateInstruction(program, fn, ip, ins); err != nil {
				return err
			}
		}
	}
	if !hasMain {
		return fmt.Errorf("IR has no main function")
	}
	return nil
}

func validateInstruction(program *IRProgram, fn *IRFunction, ip int, ins Instruction) error {
	switch ins.Op {
	case OpLoadLocal, OpStoreLocal:
		if ins.Arg < 0 || int(ins.Arg) >= fn.LocalSize {
			return fmt.Errorf("%s:%d: invalid local slot %d", fn.Name, ip, ins.Arg)
		}
	case OpJump, OpJumpIfFalse:
		if ins.Arg < 0 || int(ins.Arg) > len(fn.Code) {
			return fmt.Errorf("%s:%d: invalid jump target %d", fn.Name, ip, ins.Arg)
		}
	case OpCall:
		if ins.Arg < 0 || int(ins.Arg) >= len(program.Functions) {
			return fmt.Errorf("%s:%d: invalid function index %d", fn.Name, ip, ins.Arg)
		}
	case OpConstBool:
		if ins.Arg != 0 && ins.Arg != 1 {
			return fmt.Errorf("%s:%d: invalid boolean constant", fn.Name, ip)
		}
	case OpConstInt:
	default:
		if ins.Arg != 0 {
			return fmt.Errorf("%s:%d: opcode %s takes no argument", fn.Name, ip, ins.Op)
		}
	}
	return nil
}
