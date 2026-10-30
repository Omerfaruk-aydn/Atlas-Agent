import { solve } from './core';
function equal(a: unknown, b: unknown) { if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(JSON.stringify({actual:a,expected:b})); }
equal(solve("12.5%"),0.125);equal(solve(" -5% "),-0.05);equal(solve("12%junk"),null);equal(solve("12"),null);
