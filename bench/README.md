# Benchmark fixtures

These directories contain deliberately incomplete task inputs, hidden checks
and reference solutions. Hidden checks only compile after the benchmark runner
copies them into the matching task workspace. They are not application tests.

The module boundary keeps these fixture fragments out of the application's
`go test ./...` and lint package discovery. Validate the tasks from the project
root with a built Atlas executable:

```sh
atlas eval bench validate bench/tasks
```

Validation must reject each initial task implementation and accept its reference
solution. See `docs/task-bench.md` for running actual model comparisons and site
design evaluations.
