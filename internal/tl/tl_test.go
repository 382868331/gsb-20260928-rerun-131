package tl_test

import (
	"bytes"
	"strings"
	"testing"

	"tl/internal/tl/interp"
	"tl/internal/tl/pipeline"
	"tl/internal/tl/vm"
)

func TestEndToEndTreeAndVM(t *testing.T) {
	src := []byte(`
fun fib(n: int) -> int {
  if (n < 2) { return n; } else { return fib(n-1) + fib(n-2); }
}
fun main() -> int {
  print(fib(10));
  let i: int = 0;
  let acc: int = 0;
  while (i < 5) {
    let j: int = i + 1;
    acc = acc + j;
    i = j;
  }
  print(acc);
  return 0;
}
`)
	want := "55\n15\n"
	runOne(t, "ok.tl", src, want, 0)
}

func TestBlockShadowing(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let x: int = 1;
  {
    let x: int = 2;
    print(x);
  }
  print(x);
  return 0;
}
`)
	runOne(t, "shadow.tl", src, "2\n1\n", 0)
}
func TestClosuresCaptureMutableVariables(t *testing.T) {
	src := []byte(`
fun makeCounter(start: int) -> fun() -> int {
  let count: int = start;
  return fun () -> int {
    count = count + 1;
    return count;
  };
}
fun main() -> int {
  let a: fun() -> int = makeCounter(10);
  let b: fun() -> int = makeCounter(100);
  print(a());
  print(a());
  print(b());
  let shared: int = 7;
  let bump: fun() -> int = fun () -> int { shared = shared + 1; return shared; };
  let read: fun() -> int = fun () -> int { return shared; };
  print(bump());
  print(read());
  let i: int = 0;
  while (i < 3) {
    let n: int = i;
    let p: fun() -> int = fun () -> int { return n; };
    print(p());
    i = i + 1;
  }
  let fact: fun(int) -> int = fun self(n: int) -> int {
    if (n < 2) { return 1; }
    return n * self(n - 1);
  };
  print(fact(5));
  return 0;
}
`)
	runOne(t, "closures.tl", src, "11\n12\n101\n8\n8\n0\n1\n2\n120\n", 0)
}

func TestClosureBudgetExhaustionIsError(t *testing.T) {
	src := []byte("fun main() -> int { let loop: fun() -> int = fun self() -> int { return self(); }; return loop(); }")
	result, err := pipeline.Compile("budget.tl", src)
	if err != nil {
		t.Fatal(err)
	}
	var tree bytes.Buffer
	_, treeErr := interp.New("budget.tl", result.Program, &tree, 20).Run()
	if treeErr == nil || !strings.Contains(treeErr.Error(), "runtime budget exceeded") {
		t.Fatalf("tree error = %v", treeErr)
	}
	var machine bytes.Buffer
	_, vmErr := vm.New(result.IR, "budget.tl", &machine, 20).RunInt()
	if vmErr == nil || !strings.Contains(vmErr.Error(), "runtime budget exceeded") {
		t.Fatalf("vm error = %v", vmErr)
	}
}
func TestMaxIntegerLiteral(t *testing.T) {
	src := []byte(`
fun main() -> int {
  print(9223372036854775807);
  return 0;
}
`)
	runOne(t, "maxint.tl", src, "9223372036854775807\n", 0)
}

func TestIllegalProgramsDoNotEmit(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"fun main() -> int { let x: int = true; return 0; }", "ok.tl:1:21"},
		{"fun main() -> int { if (1) { return 0; } return 0; }", "ok.tl:1:25"},
		{"fun main() -> int { return missing(); }", "ok.tl:1:28"},
		{"fun f(x: int, y: int) -> int { return x; } fun main() -> int { return f(1); }", "ok.tl:1:71"},
		{"fun main() -> int { while (true) {} return 0; }", ""},
	}
	for i, tc := range cases {
		var buf bytes.Buffer
		_, err := pipeline.Compile("ok.tl", []byte(tc.src))
		if err != nil {
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("case %d: got %q, want position %q", i, err, tc.want)
			}
			continue
		}
		if tc.want != "" {
			t.Fatalf("case %d: expected compile error", i)
		}
		result, err := pipeline.Compile("ok.tl", []byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		machine := vm.New(result.IR, "ok.tl", &buf, 100)
		_, err = machine.Run()
		if err == nil || !strings.Contains(err.Error(), "runtime budget exceeded") {
			t.Fatalf("case %d: got %v, want budget error", i, err)
		}
		if buf.Len() != 0 {
			t.Fatalf("infinite loop produced output: %q", buf.String())
		}
	}
}

func TestRuntimeDivisionByZeroPosition(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let x: int = 10;
  let y: int = 0;
  return x / y;
}
`)
	runOne(t, "div.tl", src, "", 0)
}

func runOne(t *testing.T, file string, src []byte, wantOut string, budget int64) {
	t.Helper()
	result, err := pipeline.Compile(file, src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var tree bytes.Buffer
	treeMachine := interp.New(file, result.Program, &tree, budget)
	treeCode, treeErr := treeMachine.Run()

	var encoded bytes.Buffer
	if err := result.IR.Encode(&encoded); err != nil {
		t.Fatal(err)
	}
	decoded, err := pipeline.ReadIR(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	var stackVM bytes.Buffer
	stackMachine := vm.New(decoded, file, &stackVM, budget)
	vmCode, vmErr := stackMachine.RunInt()

	if strings.Contains(file, "div") {
		if treeErr == nil || !strings.Contains(treeErr.Error(), "div.tl:5:10") {
			t.Fatalf("tree error = %v", treeErr)
		}
		if vmErr == nil || !strings.Contains(vmErr.Error(), "div.tl:5:1") {
			t.Fatalf("vm error = %v", vmErr)
		}
		return
	}
	if treeErr != nil || vmErr != nil {
		t.Fatalf("tree=%v vm=%v", treeErr, vmErr)
	}
	if treeCode != vmCode {
		t.Fatalf("codes %d vs %d", treeCode, vmCode)
	}
	if tree.String() != wantOut || stackVM.String() != wantOut {
		t.Fatalf("tree=%q vm=%q want=%q", tree.String(), stackVM.String(), wantOut)
	}
}
