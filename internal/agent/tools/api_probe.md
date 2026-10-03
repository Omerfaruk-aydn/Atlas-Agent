Probe a running API with GET or HEAD and verify its status and required top-level
JSON keys. URL policy and ordinary network permissions apply before any request.
Redirects are not followed. Responses are limited to 256KiB and requests to 20
seconds. HTTP failure statuses can be expected explicitly. This verifies the
specific response only; it does not certify a full API contract or authorize
mutating requests. No credentials or full response bodies are returned.
