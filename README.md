# TL：可扩展教学语言工具链

这是 Go 实现的第一条端到端通路：

```text
source.tl
  └─ lexer     字符流 -> token，负责注释、关键字、运算符和词法位置
  └─ parser    token -> AST，负责语法结构
  └─ checker   AST -> 作用域/类型信息，负责语义合法性
  └─ codegen   已检查 AST -> TLIR 栈式指令
  └─ encode    TLIR -> 带 magic/version 的二进制文件

program.tlir
  └─ tlr       decode -> validate -> 独立栈虚拟机执行

source.tl
  └─ tli       parse -> check -> 树遍历解释器（用于和 VM 对照）
```

非法程序会在 `parser` 或 `checker` 阶段停止，`tlc` 不会写出可运行结果。

## 当前语言

类型：`int`（64 位有符号整数）、`bool`（`true` / `false`）。

函数类型：`fun(int, bool) -> int`（参数类型列表加 `-> 结果`；结果省略时默认为 `int`）。

语句：

- `let x: int = expr;` 声明并初始化局部变量。
- `x = expr;` 给已存在的外层或当前作用域变量赋值。
- `if (cond) { ... } else { ... }`。
- `while (cond) { ... }`。
- `return expr;`。
- 嵌套 `{ ... }` 块，块内变量可以遮蔽外层变量。

表达式：

- 整数字面量、布尔字面量、变量、括号。
- 一元 `-`、`!`。
- 算术 `+ - * / %`。
- 比较 `< <= > >= == !=`。
- 短路逻辑 `&&`、`||`。
- 用户函数调用和递归。
- 匿名函数表达式 `fun(x: int) -> int { ... }`，即闭包。
- 对任意函数值表达式追加 `(args)` 调用，例如 `add(1)(2)`。

闭包语义：

- 闭包按词法作用域捕获变量：函数离开创建它的作用域后，捕获的变量仍然存活。
- 捕获的是变量本身而不是值的快照：共享同一变量的多个闭包能观察到彼此的修改。
- 每次调用外层函数都会得到一套全新的捕获存储，不同调用产生的闭包互不串值；循环体内每次迭代声明的变量也各自独立。
- 被捕获的变量在编译期提升为堆上的 cell（checker 标记、codegen 生成 `OpWrapCell` 等指令）；VM 帧栈升级为带 tag 的值（整数 / cell / 闭包），解析器与检查器不需要知道 VM 表示。
- `let f: fun(int) -> int = fun(n: int) -> int { ... f(n - 1) ... };` 中变量先声明再检查函数体，因此闭包可以递归调用自己。
- 顶层函数名可以直接作为值使用，例如 `let f: fun(int) -> int = fib;`。
- 函数值不支持 `==` / `!=` 比较，也不能传给 `print`。

内置函数：

- `print(x: int) -> int`：打印整数，返回 `0`。
- `print_bool(x: bool) -> int`：打印 `true` 或 `false`，返回 `0`。

程序必须声明 `main() -> int`，其返回值作为进程退出码。

函数语法：

```tl
fun fib(n: int) -> int {
  if (n < 2) {
    return n;
  } else {
    return fib(n - 1) + fib(n - 2);
  }
}

fun main() -> int {
  print(fib(10));
  return 0;
}
```

## 命令

只做解析和类型检查：

```bash
go run ./cmd/tlc -check examples/fib.tl
```

编译成可保存的中间指令：

```bash
go run ./cmd/tlc -o /tmp/fib.tlir examples/fib.tl
```

由独立执行器加载运行；执行器不读取源码、不依赖 AST：

```bash
go run ./cmd/tlr -source examples/fib.tl /tmp/fib.tlir
```

直接在 AST 上解释，便于教学对照：

```bash
go run ./cmd/tli examples/fib.tl
```

限制运行预算，防止无限循环：

```bash
go run ./cmd/tlr -budget 1000 /tmp/fib.tlir
go run ./cmd/tli -budget 1000 examples/fib.tl
```

预算按执行步骤计数：VM 是指令数，树解释器是求值/执行节点数。超过预算会带源码行号报错且不返回成功状态。

## 错误位置

编译错误和运行时错误都使用 `file:line:column` 或 `file:line:1`。TLIR 保存每条指令的源行号；`tlr -source path` 可让运行时错误显示原始源文件名。

示例：

```text
examples/bad.tl:3:12: cannot initialize int variable "x" with bool value
examples/prog.tl:8:1: runtime error: division by zero
```

## 扩展路线

- 新增 token/关键字：`internal/tl/token`、`internal/tl/lexer`。
- 新增语法形式：`internal/tl/ast`、`internal/tl/parser`。
- 新增作用域或类型规则：`internal/tl/checker`。
- 新增中间操作：先扩展 `internal/tl/ir` 的 opcode 和二进制版本，再改 `codegen`、`vm`。
- 保持双执行器同步：新增语义时同时更新 `internal/tl/interp` 与 `internal/tl/vm`，并在测试中比较输出。
- 新增运行时数据结构时，VM 不能再只用 `int64` 标量，应把帧栈升级为带类型 tag 的值；解析器和检查器仍不需要知道 VM 表示。

## 测试

```bash
go test ./...
```

测试覆盖递归、局部遮蔽、短路逻辑、循环预算、非法程序拒绝、除零错误位置，以及 TLIR 二进制编码/解码后两条执行路径输出一致。

