Find and load tools that are enabled for this agent but whose schemas are not
loaded yet. Search by capability ("browser", "git diff", "desktop window",
"test coverage", "tarayıcı", "masaüstü") or load exact names from the deferred
tool index with {"names":["browser"]}. A query made only of exact tool names
loads just those tools; related tools are listed as suggestions.

Results are short summaries, not schemas. In deferred mode, results marked
loaded=true become callable on your NEXT step with their complete schemas;
then call the tool directly. Do not guess the arguments of a tool whose schema
you have not seen.

Loading only makes a schema visible. It never runs the tool, grants a
permission or enables a disabled tool: approvals, hooks and workspace limits
still apply when you call it. Disabled tools and other agents' tools are never
returned. Example: {"query":"browser navigate click"} -> next step call browser.
