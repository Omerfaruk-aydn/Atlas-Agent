import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
const a: [number,number][]=[[5,8],[1,3],[3,6],[9,9]];equal(solve(a),[[1,8]]);equal(a,[[5,8],[1,3],[3,6],[9,9]]);
