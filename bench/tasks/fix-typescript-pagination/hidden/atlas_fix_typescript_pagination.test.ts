import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
equal(solve([1,2,3,4,5],2,2),[3,4]);equal(solve([1,2],0,2),[1,2]);equal(solve([1],1,0),[]);
