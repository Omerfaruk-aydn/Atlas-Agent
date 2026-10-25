export function solve(xs: string[]): Record<string,number> { const out: Record<string,number>={};for(const x of xs)out[x]=(out[x]||0)+1;return out; }
