package tl

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const validProgram = `
fn factorial(n: int) -> int {
  if n <= 1 { return 1; } else { return n * factorial(n - 1); }
}
fn main() -> int {
  let total = 0;
  let i = 1;
  while i <= 5 {
    total = total + factorial(i);
    i = i + 1;
  }
  print total;
  return 0;
}
`

func TestPipelineMatchesInterpreter(t *testing.T) {
	checked, ir := compileForTest(t, validProgram)
	var vmOutput bytes.Buffer
	vm, err := NewVM(ir, 100000, &vmOutput)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := vm.Run(); err != nil || code != 0 {
		t.Fatalf("vm code=%d err=%v", code, err)
	}

	var directOutput bytes.Buffer
	interpreter, err := NewInterpreter(checked, 100000, &directOutput)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := interpreter.Run(); err != nil || code != 0 {
		t.Fatalf("interpreter code=%d err=%v", code, err)
	}

	if vmOutput.String() != directOutput.String() || vmOutput.String() != "153\n" {
		t.Fatalf("unexpected output: vm=%q interpreter=%q", vmOutput.String(), directOutput.String())
	}
}

func TestScopeShadowingAndAssignment(t *testing.T) {
	source := `
fn main() -> int {
  let x = 1;
  {
    let x = 2;
    print x;
  }
  print x;
  return 0;
}
`
	_, ir := compileForTest(t, source)
	var output bytes.Buffer
	vm, err := NewVM(ir, 10000, &output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.Run(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "2\n1\n" {
		t.Fatalf("unexpected output %q", output.String())
	}
}

func TestErrorsIncludeSourcePosition(t *testing.T) {
	source := `fn main() -> int {
  let x = missing;
  return 0;
}
`
	_, err := buildChecked(source)
	if err == nil || !strings.Contains(err.Error(), "2:11") || !strings.Contains(err.Error(), "undefined variable") {
		t.Fatalf("expected located error, got %v", err)
	}
}

func TestInvalidProgramDoesNotGenerate(t *testing.T) {
	_, err := buildIR(`fn main() -> int { return true; }`)
	if err == nil {
		t.Fatal("expected type error")
	}
}

func TestRuntimeBudgetStopsInfiniteLoop(t *testing.T) {
	source := `fn main() -> int {
  while true {}
  return 0;
}
`
	_, ir := compileForTest(t, source)
	var output bytes.Buffer
	vm, _ := NewVM(ir, 10, &output)
	_, err := vm.Run()
	if err == nil || !strings.Contains(err.Error(), "2:3") || !strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("unexpected err %v", err)
	}
}

func TestDivisionByZeroHasPosition(t *testing.T) {
	source := `fn main() -> int {
  return 1 / 0;
}
`
	_, ir := compileForTest(t, source)
	var output bytes.Buffer
	vm, _ := NewVM(ir, 1000, &output)
	_, err := vm.Run()
	if err == nil || !strings.Contains(err.Error(), "2:12") {
		t.Fatalf("unexpected err %v", err)
	}
}

func TestSerializedIRRequiresValidation(t *testing.T) {
	_, ir := compileForTest(t, validProgram)
	ir.Functions[0].Code[0].Op = "BROKEN"
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeIR(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVM(decoded, 1000, &bytes.Buffer{}); err == nil {
		t.Fatal("invalid IR was accepted")
	}
}

func compileForTest(t *testing.T, source string) (*CheckedProgram, *IRProgram) {
	t.Helper()
	checked, err := buildChecked(source)
	if err != nil {
		t.Fatal(err)
	}
	ir, err := Generate(checked)
	if err != nil {
		t.Fatal(err)
	}
	return checked, ir
}

func buildIR(source string) (*IRProgram, error) {
	checked, err := buildChecked(source)
	if err != nil {
		return nil, err
	}
	return Generate(checked)
}

func buildChecked(source string) (*CheckedProgram, error) {
	ast, err := Parse(source)
	if err != nil {
		return nil, err
	}
	return Check(ast)
}
