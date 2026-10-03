Run bounded engineering experiments through the existing foreground bash tool.
Supply literal argv; shell expansion is never added by this tool. Commands keep
ordinary permissions, execution isolation, cancellation and accounting.

bug_reproduce requires a nonzero expected exit and a literal failure marker.
repro_minimize additionally substitutes one exact {input} argument with input
text and removes line chunks while preserving both exit and failure marker.
Repeated commands must be suitable for replay; this is not filesystem rollback.

benchmark_compare alternates baseline_argv and candidate_argv with 3-10 samples
per variant. It reports actual process wall times, mean, median and dispersion,
not CPU/allocation measurements or a claim of statistical significance. Both
commands must exit zero. Include warmup in your benchmark executable if needed.

failure_history reads up to 20 persisted experiments in this workspace. Each
record has its source fingerprint and a current_source flag. Historical results
are not proof of current correctness. Raw command output is not persisted.
