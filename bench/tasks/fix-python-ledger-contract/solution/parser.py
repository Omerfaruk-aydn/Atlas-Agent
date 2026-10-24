from decimal import Decimal, InvalidOperation
from config import MAX_RECORDS

def parse(text):
    records, seen = [], set()
    for line in text.splitlines():
        if not line.strip():
            continue
        parts = line.split(",")
        if len(parts) != 2 or not parts[0].strip():
            raise ValueError("invalid row")
        identity = parts[0].strip()
        try:
            amount = Decimal(parts[1].strip())
        except InvalidOperation as exc:
            raise ValueError("invalid amount") from exc
        if identity in seen or not amount.is_finite():
            raise ValueError("duplicate id or nonfinite amount")
        seen.add(identity)
        records.append((identity, amount))
        if len(records) > MAX_RECORDS:
            raise ValueError("batch too large")
    return records
