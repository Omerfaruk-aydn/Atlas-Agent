Run a short Starlark program that orchestrates sub-agents. Use it when the work is a pattern, not a single call: the same review across twenty files, three independent investigations whose answers you must compare, a fix that is retried until a check passes, a list of agents whose answers you merge. One program has exact control flow, where several hand-written tool calls rely on you keeping track.

The program must define `main()`; its return value is the result. Top-level code only defines things (loops and assignments belong inside functions). Starlark is Python-like: `def`, `for`, `if`, list and dict comprehensions, `lambda`, string formatting with `%` and `.format()`. There is no file, network, clock or import access; the only way out is `agent()`.

Builtins:
- `agent(prompt, agent_name="", schema=None)`: run one sub-agent and wait. Returns its reply as a string, or, when `schema` (a JSON Schema as a dict) is given, the validated reply parsed into dicts and lists. The agent sees only the prompt, so include what it needs. A failed run raises and stops the script.
- `parallel([lambda: agent(...), ...])`: run the functions at the same time; results come back in order. A branch that fails does not stop the others: its slot holds a struct with an `error` field (`hasattr(r, "error")`). Branches cannot change shared values (a captured list is frozen); return values instead.
- `log(...)`, `phase(name)`: add to the log shown with the result. Use `phase` to mark stages.
- `agents_used()`: how many agents have started.
- `sum(values, start=0)` (Starlark has none built in), `json.encode(x)`, `json.decode(s)`, `struct(a=1)`.

Limits: at most `max_agents` agents (default 16, hard cap 64), a step budget against endless loops, and `timeout_seconds` for the whole run. Scripted agents are ordinary sub-agents: they count against the concurrent sub-agent limit, ask the user for permissions like any other, and never get the script tool themselves.

Use `schema` whenever your script reads a field of an answer (`r["verdict"]`); a free-text reply cannot be indexed. A schema checks the shape of an answer, not whether it is true.

Example: review every changed file in parallel, then fix the ones that failed.

```
SCHEMA = {"type": "object", "required": ["ok", "issues"],
          "properties": {"ok": {"type": "boolean"}, "issues": {"type": "array", "items": {"type": "string"}}}}

def review(path):
    return lambda: agent("Review " + path + " for bugs. Report ok=false with the issues.", agent_name="reviewer", schema=SCHEMA)

def main():
    files = ["a.go", "b.go", "c.go"]
    phase("review")
    results = parallel([review(f) for f in files])
    bad = [f for f, r in zip(files, results) if not hasattr(r, "error") and not r["ok"]]
    log("needs work:", bad)
    return {"needs_work": bad, "reviewed": len(files)}
```
