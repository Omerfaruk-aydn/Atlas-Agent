// The arithmetic number fields accept, as design tools do: "100/2", "24+8".

/** Evaluates + - * / and parentheses over numbers; NaN for anything else. */
export function arithmetic(expr: string): number {
  const src = expr.replace(/\s+/g, "");
  let i = 0;
  const peek = () => src[i];
  const number = (): number => {
    if (peek() === "(") {
      i++;
      const v = sum();
      if (peek() !== ")") return NaN;
      i++;
      return v;
    }
    if (peek() === "-") {
      i++;
      return -number();
    }
    if (peek() === "+") {
      i++;
      return number();
    }
    const m = /^(\d+\.?\d*|\.\d+)/.exec(src.slice(i));
    if (!m) return NaN;
    i += m[0].length;
    return Number(m[0]);
  };
  const product = (): number => {
    let v = number();
    while (peek() === "*" || peek() === "/") {
      const op = src[i++];
      const r = number();
      v = op === "*" ? v * r : v / r;
    }
    return v;
  };
  const sum = (): number => {
    let v = product();
    while (peek() === "+" || peek() === "-") {
      const op = src[i++];
      const r = product();
      v = op === "+" ? v + r : v - r;
    }
    return v;
  };
  const v = sum();
  return i === src.length ? v : NaN;
}
