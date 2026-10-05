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
	vmCode, vmErr := stackMachine.Run()

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

func TestClosuresIndependentInstances(t *testing.T) {
	src := []byte(`
fun makeCounter() -> fun() -> int {
  let count: int = 0;
  let inc: fun() -> int = fun() -> int {
    count = count + 1;
    return count;
  };
  return inc;
}
fun main() -> int {
  let a: fun() -> int = makeCounter();
  let b: fun() -> int = makeCounter();
  print(a());
  print(a());
  print(b());
  return 0;
}
`)
	runOne(t, "counter.tl", src, "1\n2\n1\n", 0)
}

func TestClosuresShareCapturedVariable(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let x: int = 1;
  let get: fun() -> int = fun() -> int { return x; };
  let set: fun(int) -> int = fun(v: int) -> int { x = v; return 0; };
  set(41);
  print(get());
  x = x + 1;
  print(get());
  return 0;
}
`)
	runOne(t, "shared.tl", src, "41\n42\n", 0)
}

func TestClosureLexicalNotDynamicScope(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let x: int = 1;
  let get: fun() -> int = fun() -> int { return x; };
  let r: int = 0;
  {
    let x: int = 2;
    r = get();
  }
  print(r);
  return 0;
}
`)
	runOne(t, "lexical.tl", src, "1\n", 0)
}

func TestNestedLambdasAndPartialApplication(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let add: fun(int) -> fun(int) -> int = fun(x: int) -> fun(int) -> int {
    return fun(y: int) -> int { return x + y; };
  };
  print(add(10)(7));
  let a: int = 1;
  let mk: fun() -> fun() -> int = fun() -> fun() -> int {
    let b: int = 10;
    return fun() -> int {
      a = a + 1;
      return a + b;
    };
  };
  let f: fun() -> int = mk();
  let g: fun() -> int = mk();
  print(f());
  print(g());
  print(f());
  return 0;
}
`)
	runOne(t, "nested.tl", src, "17\n12\n13\n14\n", 0)
}

func TestRecursiveClosure(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let fact: fun(int) -> int = fun(n: int) -> int {
    if (n < 2) { return 1; }
    return n * fact(n - 1);
  };
  print(fact(6));
  return 0;
}
`)
	runOne(t, "recclosure.tl", src, "720\n", 0)
}

func TestClosuresCreatedInLoop(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let i: int = 0;
  let f0: fun() -> int = fun() -> int { return 0; };
  let f1: fun() -> int = f0;
  let f2: fun() -> int = f0;
  while (i < 3) {
    let j: int = i * 10;
    let g: fun() -> int = fun() -> int { return j; };
    if (i == 0) { f0 = g; } else { if (i == 1) { f1 = g; } else { f2 = g; } }
    i = i + 1;
  }
  print(f0());
  print(f1());
  print(f2());
  return 0;
}
`)
	runOne(t, "loopclo.tl", src, "0\n10\n20\n", 0)
}

func TestFunctionValueFromDeclaration(t *testing.T) {
	src := []byte(`
fun double(n: int) -> int { return n * 2; }
fun main() -> int {
  let f: fun(int) -> int = double;
  print(f(21));
  return 0;
}
`)
	runOne(t, "funref.tl", src, "42\n", 0)
}

func TestClosureBudgetExhaustionIsError(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let spin: fun() -> int = fun() -> int { while (true) {} return 0; };
  return spin();
}
`)
	result, err := pipeline.Compile("spin.tl", src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var treeOut bytes.Buffer
	treeCode, treeErr := interp.New("spin.tl", result.Program, &treeOut, 5000).Run()
	if treeErr == nil || !strings.Contains(treeErr.Error(), "runtime budget exceeded") {
		t.Fatalf("tree: code=%d err=%v, want budget error", treeCode, treeErr)
	}
	var encoded bytes.Buffer
	if err := result.IR.Encode(&encoded); err != nil {
		t.Fatal(err)
	}
	decoded, err := pipeline.ReadIR(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	var vmOut bytes.Buffer
	vmCode, vmErr := vm.New(decoded, "spin.tl", &vmOut, 5000).Run()
	if vmErr == nil || !strings.Contains(vmErr.Error(), "runtime budget exceeded") {
		t.Fatalf("vm: code=%d err=%v, want budget error", vmCode, vmErr)
	}
	if treeOut.Len() != 0 || vmOut.Len() != 0 {
		t.Fatalf("budget-exhausted run produced output: tree=%q vm=%q", treeOut.String(), vmOut.String())
	}
}

func TestClosureRuntimeErrorPosition(t *testing.T) {
	src := []byte(`
fun main() -> int {
  let d: fun(int) -> int = fun(n: int) -> int {
    return 100 / n;
  };
  return d(0);
}
`)
	result, err := pipeline.Compile("clodiv.tl", src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var tree bytes.Buffer
	_, treeErr := interp.New("clodiv.tl", result.Program, &tree, 0).Run()
	if treeErr == nil || !strings.Contains(treeErr.Error(), "clodiv.tl:4:12") {
		t.Fatalf("tree error = %v", treeErr)
	}
	var encoded bytes.Buffer
	if err := result.IR.Encode(&encoded); err != nil {
		t.Fatal(err)
	}
	decoded, err := pipeline.ReadIR(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	var stackVM bytes.Buffer
	_, vmErr := vm.New(decoded, "clodiv.tl", &stackVM, 0).Run()
	if vmErr == nil || !strings.Contains(vmErr.Error(), "clodiv.tl:4:1") {
		t.Fatalf("vm error = %v", vmErr)
	}
}

func TestIllegalClosurePrograms(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"fun main() -> int { let f: fun(int) -> int = fun(b: bool) -> int { return 0; }; return 0; }", "ok.tl:1:21"},
		{"fun main() -> int { let f: fun() -> int = fun() -> int { return 1; }; return f(1); }", "ok.tl:1:78"},
		{"fun main() -> int { let x: int = 1; return x(2); }", "ok.tl:1:44"},
		{"fun main() -> int { let f: fun() -> int = fun() -> int { return 1; }; print(f == f); return 0; }", "ok.tl:1:77"},
		{"fun main() -> int { let f: fun() -> int = fun() -> int { return missing; }; return 0; }", "ok.tl:1:65"},
	}
	for i, tc := range cases {
		if _, err := pipeline.Compile("ok.tl", []byte(tc.src)); err == nil {
			t.Fatalf("case %d: expected compile error", i)
		} else if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("case %d: got %q, want position %q", i, err, tc.want)
		}
	}
}
