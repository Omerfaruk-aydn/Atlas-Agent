Replace, insert, or delete an entire symbol (function, method, class, struct) by name using LSP document symbols to find exact boundaries. Prefer this over `edit` for whole-symbol changes: it eliminates whitespace-matching failures by resolving symbol ranges through the language server instead of exact text matching.

Actions:
- `replace` (default): replace the entire symbol including signature and body
- `add_before`: insert text before the symbol
- `add_after`: insert text after the symbol
- `delete`: remove the symbol entirely

Uses the negotiated UTF-8/UTF-16/UTF-32 symbol range, preserving surrounding
content and CRLF. Set `preview=true` to save the exact diff and a plan ID without
source changes. `lsp_edit_plan` inspects/applies/recoveries the saved plan. Every
actual target needs unchanged source, ownership and ordinary permission. Results
contain plan/diff references and observed edit state; apply does not certify
compilation or tests. Interrupted multi-file mutations require journal recovery.
