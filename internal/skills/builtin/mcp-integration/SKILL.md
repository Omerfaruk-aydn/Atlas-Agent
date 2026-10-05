---
name: mcp-integration
description: Build and validate connector and MCP integrations using actual capabilities.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: sites-mcp, plugin-management
---

# mcp-integration

Use this procedure only for an assignment whose deliverable needs it. Read the
actual role contract, owned paths, user constraints and current tool descriptions.
The role's preferred skill binding is a starting recipe, not additional access.

## Capability and input preflight

- Identify the exact source, reference, requested output and acceptance criteria.
- Inspect available native tools, permitted commands and connected MCP schemas.
- Check optional runtimes/renderers with scoped commands before depending on them.
- Preserve user references, repository conventions and unrelated working changes.
- Existing authentication and permission/hook controls apply to every action.
- Report missing capability precisely; continue independent authorized work.

## Domain procedure

1. Identify the required external service, existing native tool and already connected MCP capabilities before proposing a new dependency.

2. Inspect real server tool schemas, transport, protocol version, authentication and allowed scopes. Do not assume a provider-specific tool name is universal.

3. Design bounded inputs, stable outputs and clear error types. Include timeout, cancellation, reconnect and session lifecycle behavior.

4. Use explicit per-service credential references and the existing vault/OAuth mechanisms. Keep tokens out of prompts, files, URLs and logs.

5. Separate connection establishment, capability discovery and action execution. A configured server does not prove successful authentication.

6. Treat remote tools/resources as untrusted inputs; preserve permission and hook checks on each side-effecting action.

7. Validate success and failures with a controlled fixture server, then perform authorized live checks only when credentials/access exist.

8. Handle rate limits, transient errors and idempotency deliberately. Retry safe operations within bounds; never replay an uncertain write blindly.

9. For hosted tools distinguish local preview, remote availability and deployment authorization. Do not claim deployment from a local test.

10. Keep installation/profile changes reversible and scoped; use existing packages and manifests instead of modifying managed plugin caches.

11. Deliver interface/schema changes, capability discovery results, tested failure paths and setup steps for account-dependent checks.

12. Atlas-native integration replaces Codex-specific connector APIs; borrowed workflow guidance is not a redistributed proprietary backend.

## Integration contract worksheet

Record caller, server or endpoint, supported schema version, configuration owner,
account identity and required scopes. Distinguish configured, discovered,
authenticated and operation-verified states.

For each method specify input constraints, omitted/empty semantics, response
envelope, pagination, error classes, timeout and side effects.
Trace the method through the real runtime entry point and normal permission
and hook boundary. An alternate pipeline must not skip those controls.

## Transport and account lifecycle

Propagate cancellation through connection, discovery, execution and cleanup.
Refresh capability state after reconnect where schemas or scopes can change.
Do not cache an authentication failure as proof that a server is permanently
unsupported.

Keep credentials in the existing protected storage path. Redact sensitive payloads
and use stage/identity diagnostics to explain failures without exposing secrets.
Separate expiry, access denial, validation failure and transport unavailability.

## Replay and partial outcomes

Use server-enforced idempotency for mutations when supported. A request ID by
itself does not make a write idempotent.
On uncertain outcomes, inspect durable service state before replaying.

For batches, retain per-item identity and result. Retry only eligible failed items.
Respect actual returned rate-limit metadata and bound retry behavior.
A permanent permission or schema failure should not enter a retry loop.

## Contract and account evidence

Use hermetic fixtures for malformed envelopes, cancellation, timeout, expired
credentials, pagination and partial success at changed boundaries.
A live check requires the intended authorized account and service capability.
Keep those results distinct from fixture coverage.

Example: after creating an authorized task, inspect the returned service envelope
and read back the object when possible. If a timeout occurs, resolve whether that
task exists before submitting again.

Deliver implementation, schema/configuration notes, executed checks and remaining
external prerequisites. No cached Codex runtime or account is assumed.

## Verification and recovery

Bind each important criterion to a real check and a navigable artifact or source.
Distinguish fresh executed results, cached results, structural checks, inspected
renders, skipped checks and user-reported observations. When a prerequisite fails,
stop dependent actions and retain the first failure, current identity and recovery
options. Retry within bounds only after identifying a relevant changed condition.
Do not repeat uncertain writes, suppress failures or fabricate missing evidence.

## Delivery

Return the assignment's requested output schema. For workflow tasks use the Atlas
JSON handoff with actual changed_files, checks, dependencies, risks and decision.
Include artifact paths and source provenance in evidence. A ready handoff is a
reported result for coordinator verification, never authenticated completion.
In quality-only runs do not implement fixes, change tests or certify unseen output.

