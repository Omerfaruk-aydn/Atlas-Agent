Run an explicitly configured mutation engine through guarded foreground commands.
The baseline command must exit zero first. The mutation command must produce a
fresh mutation-testing-elements JSON report under this workspace's engineering
data directory. This supports Stryker-compatible reports; it does not invent
results for other formats. Install/configure your engine separately, for example
StrykerJS's JSON reporter. No dependency is installed by this tool.

Source fingerprints must match after execution, so an engine that leaves source
mutations behind cannot report success. Killed, survived, uncovered, timeout,
invalid and pending mutants are counted separately. Empty/stale reports fail.
Each command is bounded to two minutes; focus the engine on changed files.
