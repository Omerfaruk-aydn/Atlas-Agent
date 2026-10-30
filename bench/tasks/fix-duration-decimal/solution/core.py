from decimal import Decimal
import re
def milliseconds(s):
    m=re.fullmatch(r"(\d+(?:\.\d+)?)(ms|s|m)",s.strip())
    if not m: raise ValueError("invalid duration")
    n=Decimal(m[1])*{"ms":1,"s":1000,"m":60000}[m[2]]
    if n!=int(n): raise ValueError("fractional millisecond")
    return int(n)
