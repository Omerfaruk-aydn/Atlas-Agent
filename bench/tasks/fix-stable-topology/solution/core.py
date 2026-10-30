def order(graph):
    if any(d not in graph for deps in graph.values() for d in deps): raise ValueError("missing dependency")
    done=[]
    while len(done)<len(graph):
        ready=sorted(k for k,v in graph.items() if k not in done and all(d in done for d in v))
        if not ready: raise ValueError("cycle")
        done.append(ready[0])
    return done
