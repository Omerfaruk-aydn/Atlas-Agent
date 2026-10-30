from decimal import Decimal,ROUND_HALF_UP
def total(amounts):
    n=sum((Decimal(x) for x in amounts),Decimal(0))
    return format(n.quantize(Decimal("0.01"),rounding=ROUND_HALF_UP),".2f")
