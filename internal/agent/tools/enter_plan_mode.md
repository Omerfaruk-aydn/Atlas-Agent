Switch to plan mode: from now on you can read and search but not write, edit or run anything, until the user approves your plan.

Use it when the work is large or risky enough that the user should see the approach before anything changes: a new application, a cross-cutting refactor, a migration, a design with several reasonable shapes. Do not use it for a small fix or when the user asked you to just do it.

After this call, explore with the read-only tools, write the plan out as text, then call `exit_plan_mode` with it. The user's approval turns plan mode off.

It is only available when permissions are asked for in the normal way; when the user chose to let you work without approval prompts, plan on your own and carry on.
