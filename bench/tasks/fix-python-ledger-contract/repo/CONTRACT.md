# Batch contract

Each nonblank row is `id,amount`; IDs and amounts may have surrounding spaces.
Amounts are exact signed decimals. Reject duplicate IDs after trimming and reject
malformed rows. Return independent records in input order. Sum all amounts
exactly and round the final sum once to cents using ROUND_HALF_UP. Keep the
maximum batch length at 64 records. Do not change config.py to bypass a bug.
