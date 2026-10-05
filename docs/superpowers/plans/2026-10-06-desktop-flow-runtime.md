# Desktop flow runtime

Implement in the current checkout, preserving existing desktop recipes.

- [x] Validate bounded acyclic flow graphs before child calls; explicit true/false branches.
- [x] Resolve application/dialog bindings and unique fresh control targets within the graph.
- [x] Persist plan fingerprints, cursor and intent under session-scoped process locks; no stored input text or HWND reuse.
- [x] Verify targeted native checkpoint readbacks; optional target crop only for diagnostics.
- [x] Classify reads, replace-value operations and effects; reconcile replacement outcomes without replaying uncertain effects.
- [x] Subscribe to native Windows changes before assertions, retaining deadline and bounded polling fallback.
- [x] Regression tests, descriptions, prompt guidance, lint and build candidate v15.

Compatibility: existing sequence/adaptive/transition remain available. Every
child goes through the normal dispatcher, hook and permission path. Completion
means required checkpoints passed, not that a generic UI state proves every
user acceptance criterion. Saved progress contains hashes/status only.
