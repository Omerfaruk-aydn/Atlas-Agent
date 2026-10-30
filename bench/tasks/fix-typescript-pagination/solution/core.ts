export function solve<T>(xs:T[],page:number,size:number):T[]{if(size<=0)return [];const start=(Math.max(1,page)-1)*size;return xs.slice(start,start+size);}
