def expand(text,env):
    for k,v in env.items(): text=text.replace(k,v)
    return text
