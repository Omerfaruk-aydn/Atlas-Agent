import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
equal(solve(5,100,3),40);equal(solve(5,100,10000),100);equal(solve(0,100,2),0);equal(solve(5,100,-1),0);
