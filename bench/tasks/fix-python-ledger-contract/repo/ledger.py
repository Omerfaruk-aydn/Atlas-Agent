from parser import parse

def total(text):
    return format(sum(round(amount, 2) for _, amount in parse(text)), ".2f")
