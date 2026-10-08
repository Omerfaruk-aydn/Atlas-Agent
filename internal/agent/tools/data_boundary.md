Create synthetic canaries and scan explicit project outputs for boundary leaks.

Prepare manifest: {"version":1,"action":"prepare","labels":["customer","payment"]}.
This returns a session/project-bound probe_id and random ATLAS_SYNTHETIC_ markers.
Only tool-generated fake values are accepted; do not submit real credentials.
Place these fake markers in authorized test inputs using ordinary guarded tools,
run the test, then export logs/API responses/build outputs into project files.
Preparation and injection alone do not prove a boundary passes.

Scan manifest: version 1, action scan, probe_id and targets. Each target specifies
path (file/directory), expect (absent/present) and optional labels. With no labels,
all probe labels are checked. present requires each selected marker to occur;
absent requires all selected markers absent. Include a positive control target
to demonstrate that the fixture actually exercised your canary.

Scan checks literal, base64, URL-encoded and UTF-16 LE/BE marker spellings in regular
files. Reports expose file, offset, line, label, encoding and input hashes, never
surrounding snippets. Missing/unreadable files, empty directories, symlinks and
limit exhaustion fail rather than claiming absence. Limits: 32 targets, 5000
files, 8 MiB/file, 64 MiB aggregate reads, 10000 matches. Git metadata is skipped.
Archives, hashes, encryption, arbitrary encodings and remote systems are not
decoded automatically. Use fixtures; absence from declared outputs is not proof
that an application is universally free of leaks. Probe preparation persists
per session; observed scans participate in measured verification. A passing
absence claim requires an observed present control for every selected marker.
