Move or rename a file, letting the LSP server update every reference to it elsewhere in the workspace first.

WHEN TO USE THIS TOOL:
- Moving a file to a different directory or package in a language where import paths are derived from file location (TypeScript/JavaScript relative imports, Python package moves) -- every file that imports the old path needs its import statement updated to match
- Renaming a file where other files reference it by name

HOW THIS DIFFERS FROM A PLAIN MOVE:
A plain file move (via the write or bash tools) only moves the file -- every other file still imports the old path and now has a broken reference. This tool asks the language server what needs to change *before* the move (the LSP `workspace/willRenameFiles` request), applies those edits, performs the move, and then tells the server the move happened (`workspace/didRenameFiles`) so its own model of the workspace stays correct.

PARAMETERS:
- old_path: the file's current path. Required.
- new_path: where it should end up. Required. Must not already exist.
- preview: save the exact move/reference plan and diff without changing files.

WHAT HAPPENS WHEN THE LANGUAGE DOESN'T NEED THIS:
Go's import paths are per-package (per-directory), not per-file, so moving a file within the same package changes nothing about imports elsewhere -- gopls has nothing to update, and this tool simply performs the move. A server with no file-operation support at all behaves the same way: the move happens, and the response says plainly that no cross-file edits were available (which most often means there was nothing to update, not that something went wrong). If no LSP client handles the file at all, the tool refuses rather than silently doing a plain move -- use the write or bash tools directly for that.

Every actual target, including both move paths and references, passes source,
ownership and permission checks before any file changes. The move and reference
edits share a durable journal and per-file atomic replacement. This is not one
atomic operating-system transaction. Use `lsp_edit_plan` to inspect/apply the
saved preview or recover incomplete writes; conflicting user content is retained.
Application reports actual edit state and artifact references, and certifies no
tests or independent review.
