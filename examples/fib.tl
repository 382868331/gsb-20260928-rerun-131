fun fact(n: int) -> int {
  if (n <= 1) {
    return 1;
  } else {
    return n * fact(n - 1);
  }
}

fun isEven(n: int) -> bool {
  let acc: bool = true;
  let i: int = n;
  while (0 < i) {
    i = i - 1;
    acc = !acc;
  }
  return acc;
}

fun sumTo(n: int) -> int {
  let total: int = 0;
  let i: int = 1;
  while (i <= n) {
    let add: int = i;
    total = total + add;
    i = i + 1;
  }
  return total;
}

fun main() -> int {
  print(fact(6));
  print(sumTo(10));
  print_bool(isEven(4));
  print_bool(false || (1 < 2) && !(3 == 4));
  return 0;
}
