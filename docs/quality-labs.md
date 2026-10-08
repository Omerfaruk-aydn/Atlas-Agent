# Time, fixture, invariant and data-boundary laboratories

These tools extend the existing [quality workflows](quality-workflows.md).
They are discoverable by tool_search without adding full schemas to every
initial request. Each tool takes one project-relative manifest argument,
independently of provider. Specifications reject unknown fields; generated
success text is not accepted as measured verification.

| Slash and CLI command | Agent tool | Behavior |
| --- | --- | --- |
| `/time-lab`, `atlas-agent time-lab` | `time_lab` | Advance client dates/timers and assert UI outcomes |
| `/dataset-forge`, `atlas-agent dataset-forge` | `dataset_forge` | Write deterministic related synthetic JSON fixtures |
| `/assumption-check`, `atlas-agent assumption-check` | `assumption_check` | Execute declared business invariants against JSON evidence |
| `/data-boundary`, `atlas-agent data-boundary` | `data_boundary` | Prepare synthetic markers and check output boundaries |

The CLI accepts a manifest path, `--prepare-only`, `--session ID` and `--json`.
Preparation validates declarations without launching a model or running tools.
Execution uses the ordinary agent, enabled tools, hooks and permissions.
Use `/` commands in an existing conversation to keep probe state together.
Reports persist in the session's engineering artifact store and include input
hashes. None of these commands automatically commits, pushes or releases code.

## Time laboratory

Run your fixture server and save `time.json`:

```json
{
  "version": 1,
  "url": "http://localhost:3000",
  "clock": {
    "start": "2026-12-31T23:59:00Z",
    "timezone": "Europe/Istanbul"
  },
  "cases": [{
    "name": "session expiration across midnight",
    "width": 1280,
    "height": 800,
    "steps": [
      {"action":"advance_time","advance_ms":120000},
      {"action":"assert","selector":"#session-status","condition":"text","expected":"Expired"}
    ]
  }]
}
```

```sh
atlas-agent time-lab time.json --prepare-only
atlas-agent time-lab time.json
```

Replace selectors and expectations with your application's actual contract.
The clock is installed in a fresh owned tab before application scripts. It
controls main-frame Date and function-based setTimeout/setInterval, runs due
callbacks in chronological order and drains microtasks. Timezone emulation
uses Chromium; the computer's system clock remains unchanged. New tabs share
browser cookies and remote state, so use test accounts and synthetic fixtures.

Date starts must be RFC3339 between 1970 and 2100. Each advance accepts up to
366 days, with a 10000 callback/timer budget to prevent runaway intervals.
The existing journey bounds, permissions, assertions and cleanup apply.
Callback errors or exhausted budgets fail; an unobserved advance never passes.

Animation frames, performance.now, workers, iframe clocks, networking and
server-side time remain real. Applications that combine these clocks need a
dedicated test fixture. String-based timers and custom scheduler APIs are not
supported. This is a declared client-clock rehearsal, not OS time travel.

## Related synthetic datasets

Save `dataset.json`:

```json
{
  "version": 1,
  "seed": 42,
  "output": "fixtures/commerce.json",
  "entities": [
    {
      "name": "users",
      "count": 10,
      "fields": [
        {"name":"id","kind":"id","prefix":"user_"},
        {"name":"email","kind":"email"},
        {"name":"display_name","kind":"string","empty_every":5}
      ]
    },
    {
      "name": "orders",
      "count": 40,
      "fields": [
        {"name":"id","kind":"id","prefix":"order_"},
        {"name":"user_id","kind":"reference","entity":"users","field":"id"},
        {"name":"amount_cents","kind":"int","min":0,"max":25000},
        {"name":"state","kind":"enum","values":["new","paid","cancelled"]},
        {"name":"created_at","kind":"date","start":"2026-01-01T00:00:00Z","step_seconds":3600}
      ]
    }
  ]
}
```

```sh
atlas-agent dataset-forge dataset.json
```

The ordered specification plus seed reproduce the same bytes. Reference fields
pick an ID from a previously declared entity. Forward/cyclic references are
rejected. Supported generators are id, string, email, int, bool, enum, date
and reference. null_every and empty_every explicitly create boundary cases;
these can intentionally violate business rules. Emails use example.invalid;
production data is not imported. Dates remain within 1970-2100.

Limits: 32 entities, 10000 rows/entity, 64 fields/entity, 100000 cells and 4 MiB
output. The generated output uses ordinary guarded write, file history,
permissions, hooks and assigned file ownership. Existing files must be read
before overwriting; an identical existing output is idempotent. Reports contain
seed, row counts, bytes and hashes rather than echoing every row. A generated
fixture is not evidence that the application passed a test.

## Executable business invariants

Save `assumptions.json` after generating or exporting actual test evidence:

```json
{
  "version": 1,
  "checks": [
    {"name":"unique payment identities","file":"fixtures/commerce.json","pointer":"/orders","rule":"unique","field":"/id"},
    {"name":"every order has a user","file":"fixtures/commerce.json","pointer":"/orders","rule":"references","field":"/user_id","reference_file":"fixtures/commerce.json","reference_pointer":"/users","reference_field":"/id"},
    {"name":"amounts are nonnegative","file":"fixtures/commerce.json","pointer":"/orders","rule":"range","field":"/amount_cents","min":0},
    {"name":"orders were actually exercised","file":"fixtures/commerce.json","pointer":"/orders","rule":"count","min":1}
  ]
}
```

```sh
atlas-agent assumption-check assumptions.json
```

Additional rules: exists (null allowed), nonempty (string/object/array), equals
(expected JSON), required (each row field non-null/nonempty string), count,
unique, range, sum_equals (exact decimal sum equals numeric expected) and
references. pointer/field/reference_pointer/reference_field use RFC6901: empty
selects root, `~1` escapes slash and `~0` escapes tilde. Equivalent number
spellings such as 1 and 1.0 compare equally; decimal sums avoid float rounding.

This declarative checker executes no supplied JavaScript or shell. Exercise
the application with existing guarded tools and export its data first. Reports
show failed names, rules, reasons and zero-based counterexample row indices,
without echoing values. Input hashes identify what was actually read. Each
document is cached once in a run; concurrent files are not an atomic database
snapshot. Passing fixture checks do not prove all future application behavior.

Limits: 64 checks, 32 documents, 1 MiB/document, depth 64, 100000 JSON nodes,
1024-character numeric precision and bounded numeric exponents. Missing,
malformed, oversized or unreadable inputs fail rather than claiming success.
Assertions can be included as `assumption_check` in `verify run` contracts.

## Synthetic output-boundary checks

Prepare a probe in the current conversation with `boundary-prepare.json`:

```json
{"version":1,"action":"prepare","labels":["customer","payment"]}
```

```sh
atlas-agent data-boundary boundary-prepare.json --json
```

The result contains random synthetic markers and probe_id; CLI JSON also gives
the session ID. Use those fake values in authorized test inputs. Exercise the
application, and export its logs, response bodies or build artifacts to local
project paths using existing guarded tools. No network capture or injection
is performed automatically by this scanner.

Keep an input/control file containing both markers, and save the scan manifest:

```json
{
  "version": 1,
  "action": "scan",
  "probe_id": "REPLACE_WITH_PREPARED_32_CHARACTER_ID",
  "targets": [
    {"path":"fixtures/control.txt","expect":"present"},
    {"path":"test-output/public-response.json","expect":"absent"},
    {"path":"test-output/logs","expect":"absent","labels":["payment"]}
  ]
}
```

```sh
atlas-agent data-boundary boundary-scan.json --session SESSION_ID
```

Probes are session/project bound. Each absent marker requires a passing present
control; this prevents an unexercised fixture from silently passing. Controls
prove marker presence in that declared input, not that every remote code path
consumed it. Real credentials cannot be supplied as probe markers.

The scanner detects literal, base64, URL-encoded and UTF-16 LE/BE spellings.
Reports contain file, offset, line, label, encoding, counts and input hashes;
surrounding content is never copied into results. Directories are traversed
without following symlinks; Git metadata is skipped. Missing/unreadable files,
empty targets or exhausted limits fail. Limits: 32 targets, 5000 files,
8 MiB/file, 64 MiB total reads and 10000 matches.

Encrypted, compressed, hashed or other encodings are not automatically decoded.
A passing report establishes absence only in the declared readable outputs.
Observed scans can be used as `data_boundary` verification checks. Probe
preparation alone cannot satisfy a verification contract.
