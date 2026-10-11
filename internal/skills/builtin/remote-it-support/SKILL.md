---
name: remote-it-support
description: Diagnose and fix problems on a computer connected through /support with measured evidence, owner-granted permissions and independent verification.
compatibility: Atlas remote_support tool over an accepted /support session; Windows, macOS and Linux remote computers with platform-specific commands verified per session.
metadata:
  provenance: atlas-authored
  reviewed-sources: atlas-support-protocol
---

# remote-it-support

Use this procedure when the user asks for IT support, diagnosis, a file
transfer, an installation or a configuration fix on the computer connected
through `/support`. The role's preferred skill binding is a starting recipe,
not additional access: the computer owner's grants decide what can run.
Platform command recipes live in
`atlas://skills/remote-it-support/reference.md`; read only the section for the
remote OS and the symptom at hand.

## Capability and input preflight

- Call `remote_support` `status`. Record remote_device_id, name, OS, session,
  generation, granted capabilities, allowed folders, expiry, desktop availability
  and whether the owner paused the task or screen control.
- Restate the user's problem and the target computer in one sentence. If the
  request could mean this computer or the remote one, ask; never guess.
- Missing capability: continue with what is granted; ask the owner with
  `request_permission` (a reason they can understand) only when the next
  informative step needs it. A request is not a grant.
- This computer's `bash`, `view`, `computer` and `browser` tools never act on
  the remote computer. Results from them are not remote evidence.

## Working method

1. Identify the problem and the target device from status and the user's words.
2. Observe symptoms and environment: `system_info`, the relevant logs, process,
   service or network state, a remote screen observation when the complaint is
   visual. Prefer accessibility data over screenshots.
3. Form hypotheses only from observed data, each with what would confirm or
   refute it.
4. Choose the least invasive, most informative check next (read before write,
   query before change, one variable at a time).
5. Narrow the root cause until one explanation fits all evidence, or state
   which competing explanations remain.
6. Before a change, record the prior state (current setting, version, service
   start type, file hash, free space) and how to restore it. Apply the smallest
   fix inside the granted scope.
7. Verify independently that the original complaint is gone: re-run the
   failing scenario, read the version, check the service state, open the app on
   the remote screen. Exit code 0 is not proof; an accepted install request is
   not an installation.
8. Report: what was asked, what was examined, the verified root cause (or why
   it is not verified), what changed, the final check, remaining uncertainty or
   blockers, and how to undo the change (or that it cannot be undone).

## Professional rules

- Do not present an unmeasured performance cause as the root cause; measure
  CPU, memory, disk and process data first and give the time of measurement.
- Keep "disk full" (capacity, `used_percent`) apart from "disk failing"
  (SMART, file system or controller errors in logs). Capacity data says
  nothing about drive health.
- No generic clean-ups, registry cleaners, "optimizers" or disabling security
  software as a fix.
- Data-loss risk (delete, format, overwrite, reset) only inside the granted
  scope, after recording the prior state and with the user's explicit consent.
- Install only from the vendor or a trusted package manager source; check
  publisher, version and signature or hash where the platform allows it.
- No unnecessary restarts; ask before closing the user's running applications.
- Separate application, network, user profile, permission and system resource
  causes; look for a shared dependency behind several symptoms and classify
  repeated problems.
- Every finding names its source, the time and the remote device id.
- Use support history only for facts that are current and were granted; a
  previous session's state is not today's state.
- If rollback is not supported, say so before the change.

## Recovery

- `outcome_unknown`: never resend. Read `reconciled`, or call
  `operation_status` with the operation_id once connected, or observe the
  remote state.
- `session_paused`, `control_paused`: wait for the owner; report what is pending.
- `secure_desktop` or a UAC prompt: the person at the remote computer must
  answer it. Atlas never bypasses it.
- `remote_desktop_unavailable`: the screen is locked, signed out or not
  interactive; ask the owner. Never use this computer's desktop instead.
- `session_revoked`, `session_expired`, `remote_offline`: stop remote work and
  report the state; a new session needs a new code and the owner's acceptance.
- `capability_denied`: continue with granted actions or ask; do not route the
  same effect through another command.

## Delivery and verification

Map each claim to tool evidence with its operation_id and remote_device_id.
Use "completed" only when the final check passed on the remote computer. When
the connection dropped, report the last verified state and every operation whose
outcome is unknown.
