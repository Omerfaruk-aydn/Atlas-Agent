export function solve(obj:Record<string,unknown>,keys:string[]):Record<string,unknown>{const out:Record<string,unknown>={};for(const k of keys)if(obj[k])out[k]=obj[k];return out;}
