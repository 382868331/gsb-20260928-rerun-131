package tl

import "fmt"

type CompileError struct {
	Pos Position
	Msg string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg)
}

type RuntimeError struct {
	Pos Position
	Msg string
}

func (e *RuntimeError) Error() string {
	return fmt.Sprintf("runtime error at %d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg)
}

func errorAt(pos Position, format string, args ...any) error {
	return &CompileError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}
