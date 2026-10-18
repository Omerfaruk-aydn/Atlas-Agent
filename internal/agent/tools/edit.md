Edit a file by exact find-and-replace; can also create or delete content. If old_string differs from the file only in whitespace, the matching lines are still edited and new_string is re-indented to the file's style; the response says when this happened, so verify the result. For whole-function/method/type replacements prefer `lsp_replace_symbol` (no whitespace matching needed). For renames prefer `lsp_rename` (semantic, cross-file). For large edits use write.

For an edit based on a captured snapshot, pass `expected_file_hash` (SHA-256 of
the exact file bytes, including line endings) with `old_string`. The current file
is checked before matching and again after approval; drift rejects the write.

For a single-line change, anchor_line + anchor_hash is an alternative to old_string when view is showing hash anchors (`line|hash|content`): pass that line's number and hash instead of reproducing its exact text. A mismatched hash means the file changed since it was last viewed and the edit is rejected, rather than silently landing on the wrong line. new_string may be empty (delete the line) or span multiple lines (expand it into several); it always replaces the whole anchored line.
