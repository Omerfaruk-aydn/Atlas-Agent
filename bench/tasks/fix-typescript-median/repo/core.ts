export function solve(xs: number[]): number | null { xs.sort(); return xs[Math.floor(xs.length/2)] ?? null; }
