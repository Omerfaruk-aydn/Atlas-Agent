export function solve(xs: number[]): number | null { if(!xs.length)return null;const a=[...xs].sort((a,b)=>a-b),n=a.length;return n%2?a[n>>1]:(a[n/2-1]+a[n/2])/2; }
