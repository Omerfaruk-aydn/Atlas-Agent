import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
const r=solve(["__proto__","x","__proto__"]);equal(r["__proto__"],2);equal(r["x"],1);equal(({} as Record<string,unknown>)["polluted"],undefined);
