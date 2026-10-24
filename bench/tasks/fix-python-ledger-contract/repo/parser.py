def parse(text):
    return [(parts[0], float(parts[1])) for line in text.splitlines() if line.strip() for parts in [line.split(",")]][:-1]
