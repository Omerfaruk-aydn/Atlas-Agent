def batches(xs,n):
    if n<=0: raise ValueError("size")
    return [xs[i:i+n] for i in range(0,len(xs),n)]
