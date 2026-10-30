Verify the proof tests behind security findings in the audit manifest (`.atlas/security/findings.json`).

Each finding names a Go proof test in the user's own project. The tool validates the finding, runs the proof twice, and reports a status:

- `confirmed`: the proof compiles and fails on every run, and its failure output names the finding id in square brackets, for example `t.Errorf("[V1] ...")`.
- `unconfirmed`: anything else, with the reason (no proof, does not compile, passes, skipped, flaky, fails for an unrelated reason, fails unconditionally, file outside the project).

Use it while writing proofs to check a proof fails for the right reason before reporting the finding. The status is always computed from the test result; any status written into the manifest is ignored. Never describe a finding as confirmed unless this tool says so.

Parameters: `finding_id` limits the check to one finding; `manifest` overrides the manifest path.
