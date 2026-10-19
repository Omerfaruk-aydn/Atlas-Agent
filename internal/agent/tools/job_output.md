Read stdout/stderr from a background shell by its ID.

By default this returns immediately with whatever the job has produced so far, and says whether it is still running.

The first read returns all output. Later reads return only what the job printed since your previous read, with a note giving the number of earlier lines and the log file under `.atlas/logs` that holds the full output; view or grep that file when you need the earlier part again. After a finished job has been read once, the next read returns all of its output again.

Set `wait=true` only for a job that is expected to finish on its own -- a build, a test run, a migration, an install. The wait is bounded: it ends when the job finishes or after `wait_timeout` seconds (default 30, max 600), whichever comes first, and then returns the output collected so far.

Do NOT set `wait=true` for a job that runs until stopped: dev servers, `watch` modes, REPLs, tunnels, log tails. They never finish, so waiting only burns the timeout and tells you nothing extra. Read their output with `wait=false` (poll again if you need more), and call `job_kill` when you are done with them.
