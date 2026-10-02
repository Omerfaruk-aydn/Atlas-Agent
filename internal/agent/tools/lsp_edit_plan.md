Inspect, apply or recover a saved semantic edit plan. Plan IDs come from
`lsp_rename`, `lsp_rename_file` or `lsp_replace_symbol` with `preview=true`.
Inspect returns exact target paths and immutable plan/diff artifact references.
Apply validates every target's current source, ownership and ordinary permissions
before changing any file. Multi-file changes use per-file atomic replacement
and a recovery journal; they are not one atomic operating-system transaction.
Recovery only restores files whose current hashes still match recorded writes.
Other user changes are preserved and reported as conflicts. An unresolved journal
blocks task/stage certification and resume. Applying edits never certifies tests
or independent review. Read-only specialists may inspect but cannot apply/recover.
