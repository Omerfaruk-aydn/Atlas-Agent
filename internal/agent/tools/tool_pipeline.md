Run a bounded declarative tool program. Supply steps with unique id, an allowed tool name and structured arguments. An optional items array repeats a step with the exact string "$item" substituted in arguments; no shell interpolation is performed. if_success may reference a preceding step. return selects the step outputs to expose.

Every invocation uses the current authorized tool palette, schema validation, ordinary hooks, ownership, permissions, execution policy, timeout and journal. A denial, halt or error stops the program; calls are not replayed. Tool pipelines cannot invoke themselves, goals, job scheduling or task-board controls. Limits: 32 steps, 64 invocations, 5 minutes, 8 KiB content per result and 64 KiB final output. Outputs are observations, not proof that the requested goal was met.

This is a JSON tool program, not arbitrary Python/JavaScript execution. Use explicit tool arguments and independent assertions after mutations.

