# Claude subscription tool contract

Claude subscription requests advertise local function tools with a wire prefix,
for example local `computer` becomes `_computer`. Responses must translate back
before entering the agent dispatcher. The adapter now applies the same mapping
to Generate responses, streaming tool-input names, completed streamed calls,
tool choice and outgoing conversation history. Stored local history stays local.

Mappings only include function tools advertised on that call. Unknown names and
provider-executed tools are preserved; prefixes are never stripped arbitrarily.
Tools whose local names already begin with an underscore retain exact identity.

Some responses encode typed argument fields as JSON strings, such as
`automation:"{\"window_id\":\"11\"}"` or `argv:"[\"command\"]"`.
The adapter decodes these only where the tool schema declares object/array,
or an exact safe integer. Text and unknown fields remain unchanged. It does not
invent missing values, repair shell commands or round fractional coordinates.
The complete candidate must validate against the advertised schema before it
replaces the original input. Calls remain under ordinary hooks and permissions.

Regression tests exercise HTTP Generate and SSE streaming responses, wire tool
choice, immutable local history, collisions, object/array decoding, coordinates,
unchanged text, malformed JSON, missing fields and rejected schema bounds.

The 2026-10-06 benchmark session `af54fc10-6487-410c-812b-7b6649d3bb69`
showed unknown prefixed tools and typed-field decoding failures. It also used
shell scripts for creation and closing, so its reported 2:54 duration is not an
equivalent desktop-only benchmark. Prompt guidance now keeps GUI assignments in
the requested applications and dependent actions in ordered pipelines.
