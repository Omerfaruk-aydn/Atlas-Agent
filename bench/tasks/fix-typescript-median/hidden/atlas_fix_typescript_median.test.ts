import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
const a=[10,2,3,4];equal(solve(a),3.5);equal(a,[10,2,3,4]);equal(solve([]),null);
