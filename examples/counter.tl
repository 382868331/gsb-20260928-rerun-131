// Closures with mutable captured variables.
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
