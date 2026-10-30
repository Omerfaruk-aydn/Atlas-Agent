import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
equal(solve({a:0,b:false,c:""},["a","b","c","toString"]),{a:0,b:false,c:""});
