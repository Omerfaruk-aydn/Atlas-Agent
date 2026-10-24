def flatten(xs):
    out=[]
    for x in xs:
        out.extend(flatten(x)) if isinstance(x,(list,tuple)) else out.append(x)
    return out
