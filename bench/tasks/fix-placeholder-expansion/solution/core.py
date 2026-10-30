import re
def expand(text,env):
    return re.sub(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}",lambda m:env.get(m[1],m[0]),text)
