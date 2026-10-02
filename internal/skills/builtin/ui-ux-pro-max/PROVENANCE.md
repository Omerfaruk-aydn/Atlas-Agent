# Reference provenance

Source: user-provided `claude-skills.zip`, imported on 2026-10-01.
Archive SHA-256: `495c385ff106e5d4e14c40603e435341dc3840a886d6ba7f50763791bfd7efa8`.
No upstream author or license was supplied in the archive; no license grant or
official endorsement is asserted. Confirm redistribution rights before a public
release of these reference assets.

All 24 CSV files are preserved byte-for-byte from `ui-ux-pro-max/data/`.
SOURCE.md is the original supplied SKILL.md, preserved for attribution and
comparison. It is archival data, not the active workflow. The integration's
SKILL.md replaces filesystem/Python execution with the offline read-only
design_search tool and ordinary permission-checked project writes.

Python scripts and generated __pycache__ files are intentionally not shipped.
Native deterministic lexical BM25 replaces the script search. Design-system
mode returns candidates for agent synthesis; it does not claim to reproduce
the supplied Python generator, run parallel searches or save files. Numeric,
framework, accessibility and marketing claims in source rows require validation
against the actual implementation and relevant current primary documentation.

Some supplied CSV examples have literal unescaped quotes or field counts that
differ from their headers. Search accepts literal quotes; mismatched rows are
returned as unstructured reference text with a data warning rather than assigning
potentially incorrect column labels. The originals are not silently repaired.
