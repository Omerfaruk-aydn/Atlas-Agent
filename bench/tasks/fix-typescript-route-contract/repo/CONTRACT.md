# Route contract

Absolute paths are case-sensitive. Strip a query or fragment before splitting
segments. Decode percent escapes once per segment, rejecting invalid escapes or
an encoded slash. Collapse repeated slashes and remove a trailing slash except
for root. Preserve dot segments literally; this is not filesystem normalization.
Both registration and lookup must use the same canonical identity. Reject a
duplicate canonical registration and keep at most 64 routes. Do not change the
configuration limit. Lookup of a missing route returns undefined.
