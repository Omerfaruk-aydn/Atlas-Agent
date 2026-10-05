## Establish the real service contract

Use mcp-integration for actual native or connected capabilities.
Discover supported schemas and resolve the intended authenticated identity.
Distinguish configuration, discovery, authentication and verified operation.

Preserve normal hooks, permissions, credential handling and cancellation.
Classify input validation, service errors and transport failures separately.
Keep secrets out of diagnostics and prompts.

## Exercise lifecycle and side effects

Check success envelopes, malformed input, timeout, cancellation and reconnect
at the changed boundary with controlled fixtures.
Mutations require server-enforced idempotency or prior-outcome inspection before
retry. Track partial batches per item rather than replaying successful writes.

Use live-account checks only within authorized scope and actual capability.
Report fixture results separately from live results and local preview separately
from deployment. Identify exact remaining prerequisites rather than describing
an untested configured server as working.
