Rename a symbol across files using LSP rename. The runtime validates every actual
target, source hash, task ownership and ordinary permission before changing files.
Set `preview=true` to save an exact diff and plan without source changes. Use
`lsp_edit_plan` to inspect/apply the saved plan or recover interrupted writes.
Application uses per-file atomic replacement and a recovery journal. Changed
sources or user-written content block application/recovery rather than being
overwritten. Results contain plan/diff artifact references and observed edit state;
successful application does not certify compilation, tests or independent review.
