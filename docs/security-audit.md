# Security audit with proofs

`atlas-agent security audit [path]` audits a project you own. Its one rule:
a finding is **confirmed** only when a proof test in your project fails,
repeatably, for the reason the finding states. Everything else is reported
as **unconfirmed** and is never shown as confirmed.

Only audit code you own or are authorized to test. Proof tests are ordinary
Go tests that run in your project; the agent is instructed to use `httptest`
and `t.TempDir` and never to touch the network or paths outside the
temporary directory. Review proof files like any other code.

## Usage

```
atlas-agent security audit                # threat model, agent discovery, verify proofs
atlas-agent security audit ./internal/api # limit discovery to a directory
atlas-agent security audit --fix          # fix confirmed findings, re-verify
atlas-agent security audit --no-agent     # no model: re-verify the manifest, report static leads
atlas-agent security audit --json --fail-on high
```

Flags: `--fix`, `--no-agent`, `--skip-deps` (skip govulncheck), `--json`,
`--fail-on <severity>` (non-zero exit while a confirmed finding at or above
that severity is open; unconfirmed leads never fail the gate), `--model`,
`--proof-timeout`.

Agent passes run as `atlas-agent run --role security` child processes, so
they use your configured models and permission mode. The `security` role
and the `security_verify` tool are also available in normal sessions.

## How it works

1. **Threat model.** A deterministic scan lists entry points (HTTP, CLI,
   env, files, deserialization), sinks (exec, SQL, file writes, outbound
   requests), trust boundaries and hotspot files. Saved to
   `.atlas/security/threat-model.json`.
2. **Discovery.** The security agent reads the hotspots and, for each
   vulnerability, writes a Go proof test (`securityproof_<ID>_test.go`,
   `TestSecurityProof_<ID>`) plus `.atlas/security/findings.json` with CWE,
   severity, attack path and remediation.
3. **Verification.** For every finding the proof must be a `_test.go` file
   inside the project, declare the named test, not fail unconditionally,
   compile, fail on both of two runs, and mention `[<ID>]` in its failure
   output. Any status or evidence written in the manifest is discarded.
4. **Other leads.** Existing static analysis (`security_scan`) and
   dependency advisories (govulncheck via `depaudit`) are added as
   unconfirmed findings, with CWE ids (CWE-798, 327, 295, 89, 78, 1395).
   A missing govulncheck is stated in the report, not hidden.
5. **Fix (`--fix`).** The baseline suite is recorded, the agent fixes the
   confirmed findings, then each proof must be byte-identical (hash
   checked), now pass, and no previously passing test may fail. Otherwise
   the finding is `fix_failed`. Proofs remain as regression tests.

## Output

`.atlas/security/report.md` and `report.json`, also printed. Sections:
confirmed, fix failed, fixed, unconfirmed (marked NOT demonstrated), then
trust boundaries and limits. Findings are ranked by status then severity.

## Limits

- Proofs are verified for Go only. In other languages findings stay
  unconfirmed and the report says so.
- Threat-model signals are pattern matches, not data-flow analysis.
- A proof that fails for the right reason shows the weakness exists; it
  does not size the real-world impact.
