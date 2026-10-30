export function solve(s:string):number|null{const m=/^\s*([+-]?(?:\d+(?:\.\d+)?|\.\d+))%\s*$/.exec(s);if(!m)return null;const n=Number(m[1])/100;return Number.isFinite(n)?n:null;}
