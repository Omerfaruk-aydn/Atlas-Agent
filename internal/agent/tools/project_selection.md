Select focused context or candidate tests from a refreshed bounded project map.
context_select ranks direct changed files, Go source graph dependencies,
same-directory files and query matches. Graph limitations are returned explicitly.
token_budget (128-16000) uses a conservative estimate of one token per two UTF-8
bytes plus metadata. This is an estimate, not provider token accounting.
Returned snippets are capped by the budget and their source hashes are verified.

test_select returns likely related tests using Go graph edges, exact changed
tests and directory proximity, with reasons. This is partial: runtime dependency and
coverage information are not inferred. full_suite_required remains true until
authoritative dependency/coverage evidence establishes a narrower safe scope.
No command is executed by selection.

contract_diff compares two bounded workspace-relative OpenAPI 3.x JSON files.
It reports structural changes as requiring review or potentially breaking,
excluding descriptive fields. References and full schema compatibility are not
resolved; even an empty structural diff is not a semantic compatibility proof.
