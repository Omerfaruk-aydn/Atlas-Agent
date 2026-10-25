import copy
def merge(a,b):
    out=copy.deepcopy(a)
    for k,v in b.items():
        out[k]=merge(out[k],v) if isinstance(out.get(k),dict) and isinstance(v,dict) else copy.deepcopy(v)
    return out
