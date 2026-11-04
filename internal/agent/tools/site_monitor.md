Check the sites the Atlas voice service monitors, or ask to be called when one recovers.

- `status` lists each configured site with its health (unknown, healthy, degraded, down, recovered), consecutive failures, last error and whether a recovery call is armed.
- `notify_on_recovery` arms a call for when a site that is down comes back (use it when the owner says "tell me when it's back"). The flag clears itself after the recovery call.

Only sites from the configuration can be monitored; this tool cannot add URLs. The owner is called on state changes only, never on every failed check.
