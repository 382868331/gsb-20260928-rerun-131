# TeachingLang

这是一条分阶段教学编译工具链：

1. `internal/tl/lexer.go` 和 `parser.go` 生成带源位置的 AST。
2. `checker.go` 管理函数表和嵌套局部作用域，完成静态类型检查。
3. `codegen.go` 将 AST 降低为 JSON 栈式中间指令。
4. `vm.go` 独立校验并执行保存后的中间指令。
5. `interpreter.go` 直接遍历类型检查后的 AST，用于与 VM 输出对照。

## 示例

```go
fn factorial(n: int) -> int {
  if n <= 1 {
    return 1;
  } else {
    return n * factorial(n - 1);
  }
}

fn main() -> int {
  let x = 5;
  print factorial(x);
  return 0;
}
```

```bash
go run ./cmd/tl compile -o program.tli program.tl
go run ./cmd/tl run -budget 1000000 program.tli
go run ./cmd/tl interpret -budget 1000000 program.tl
```

`main` 必须是无参数且返回 `int`；非零返回值会成为进程退出码。VM 会在加载外部 IR 时重新校验，非法中间文件不能运行。

