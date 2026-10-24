def mask(headers):
    return {k:("[REDACTED]" if k.lower() in ("authorization","cookie") else v) for k,v in headers.items()}
