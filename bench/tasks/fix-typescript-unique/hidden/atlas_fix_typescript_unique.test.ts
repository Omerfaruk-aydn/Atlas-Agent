import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
equal(solve(["b","a","b",""]),["b","a",""]); equal(solve([]),[]);
