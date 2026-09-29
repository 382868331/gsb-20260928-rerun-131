package pipeline

import (
	"io"

	"tl/internal/tl/ast"
	"tl/internal/tl/checker"
	"tl/internal/tl/codegen"
	"tl/internal/tl/ir"
	"tl/internal/tl/parser"
)

type Result struct {
	Program *ast.Program
	Checked *checker.Result
	IR      *ir.Program
}

func Parse(file string, src []byte) (*ast.Program, error) {
	return parser.Parse(file, src)
}

func Check(file string, prog *ast.Program) (*checker.Result, error) {
	return checker.Check(file, prog)
}

func Compile(file string, src []byte) (*Result, error) {
	parsed, err := parser.Parse(file, src)
	if err != nil {
		return nil, err
	}
	checked, err := checker.Check(file, parsed)
	if err != nil {
		return nil, err
	}
	generated := codegen.Generate(parsed, checked)
	return &Result{Program: parsed, Checked: checked, IR: generated}, nil
}

func WriteIR(w io.Writer, p *ir.Program) error { return p.Encode(w) }

func ReadIR(r io.Reader) (*ir.Program, error) { return ir.Decode(r) }
