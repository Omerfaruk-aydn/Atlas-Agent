# Task-scoped tool routing

Atlas keeps the core tool schemas and a compact capability index available.
Clear browser, desktop, Git, test, lint, coverage, memory and pipeline requests
preload small relevant tool groups. Turkish desktop application names and
requests to open a YouTube video or an HTTP URL also select the relevant group.
These hints expose schemas only; they do not execute tools or grant permission.
Disabled tools remain excluded by the live authorized palette.

Every advertised palette receives a compact routing/recovery protocol. Specific
category guidance is included only for categories represented in that palette.
Browser and desktop protocols arrive with their respective tools, including tools
loaded during a conversation through `tool_search`. Ordinary greetings still omit
the large browser, desktop and pipeline schemas.

The protocol directs models to use Atlas's enabled tools rather than install
packages with similar names. It requires scoped discovery, cancellation-aware
recovery, fresh targets, ordered dependent actions and evidence of the outcome.
Browser startup recovery may use one close/navigation cycle while the task is
active; it must not restart canceled work or discard a working session for an
ordinary missing element. Unrelated credential files are not troubleshooting
inputs. Media playback and playback quality must be verified separately.

Google's adapter preserves all consecutive leading system messages, so dynamically
attached routing and desktop/browser guidance reaches its system instruction.
System messages after conversation content remain excluded by that adapter.

Regression coverage includes the real Turkish YouTube request across OpenAI,
Anthropic and Google request formats, intent positives/negatives, category-scoped
guidance, provider serialization, deferred palette size, session isolation and
disabled-tool filtering. Mock-provider tests verify request construction; they
do not establish live-model task accuracy or a measured runtime improvement.
