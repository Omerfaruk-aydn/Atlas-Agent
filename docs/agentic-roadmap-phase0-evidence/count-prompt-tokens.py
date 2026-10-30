"""Count fixed rendered system messages without contacting a model provider."""

import importlib.metadata
import json
import pathlib
import sys

import tiktoken


source = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])
samples = json.loads(source.read_text(encoding="utf-8-sig"))
encoding = tiktoken.get_encoding("o200k_base")
for sample in samples:
    messages = sample.pop("messages")
    sample["o200k_base_tokens"] = sum(
        len(encoding.encode(message, disallowed_special=())) for message in messages
    )

report = {
    "fixture_date": "2026-10-27",
    "encoding": "o200k_base",
    "tiktoken_version": importlib.metadata.version("tiktoken"),
    "atlas_estimator": "sum(ceil(UTF-8 message bytes / 4))",
    "scope": (
        "Rendered system message text only; no tool schemas or provider chat framing. "
        "Fixed metadata; empty skills, memory, global context, git status, dynamic "
        "session blocks. project_agents adds root AGENTS.md. Tool protocols match "
        "browser/computer-only palettes. No provider calls."
    ),
    "samples": samples,
}
destination.write_text(
    json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8"
)
