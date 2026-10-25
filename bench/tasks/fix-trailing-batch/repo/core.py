def batches(xs,n):
    return [xs[i:i+n] for i in range(0,len(xs)-n+1,n)]
