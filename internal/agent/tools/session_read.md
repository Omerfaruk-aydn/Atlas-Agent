Read an earlier session in this workspace, a page at a time, or list the most recent ones.

- Call it with no arguments to see the 8 most recent earlier sessions (id, title, date, size).
- Call it with a `session_id` (from that list or from `session_search`) to read its messages. Each page holds up to 40 messages, each cut to about 1500 characters; follow `next_offset` for the next page.

Use it when the user refers to earlier work ("like we did yesterday", "continue that migration") and you need what was actually said, not a guess. It returns prose only; tool output is not included. Do not read whole sessions speculatively: find the right one with `session_search` first.
