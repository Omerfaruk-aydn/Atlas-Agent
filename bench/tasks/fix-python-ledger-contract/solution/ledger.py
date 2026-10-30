from decimal import Decimal, ROUND_HALF_UP
from parser import parse

def total(text):
    return format(sum((amount for _, amount in parse(text)), Decimal(0)).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP), ".2f")
