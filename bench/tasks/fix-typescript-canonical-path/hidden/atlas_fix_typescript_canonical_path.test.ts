import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
equal(solve("/a//b/../c/."),"/a/c");equal(solve("/../../x"),"/x");let rejected=false;try{solve("a/b")}catch{rejected=true}equal(rejected,true);
