import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
equal(solve("  Café & Tea! "),"cafe-tea");equal(solve("a___b"),"a-b");
