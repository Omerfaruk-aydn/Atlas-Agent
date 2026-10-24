def mask(headers):
    return {k:("[REDACTED]" if k=="Authorization" else v) for k,v in headers.items()}
