package tlerr

import "fmt"

import "tl/internal/tl/token"

// Error is a compiler or runtime error anchored at a source position.
type Error struct {
	File string
	Pos  token.Pos
	Msg  string
}

func (e *Error) Error() string {
	file := e.File
	if file == "" {
		file = "<input>"
	}
	return fmt.Sprintf("%s:%s: %s", file, e.Pos, e.Msg)
}

func At(file string, pos token.Pos, format string, args ...any) *Error {
	return &Error{File: file, Pos: pos, Msg: fmt.Sprintf(format, args...)}
}
