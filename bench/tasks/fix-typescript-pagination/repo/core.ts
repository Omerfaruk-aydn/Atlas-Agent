export function solve<T>(xs:T[],page:number,size:number):T[]{return xs.slice(page*size,(page+1)*size);}
