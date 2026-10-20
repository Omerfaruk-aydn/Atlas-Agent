import { existsSync } from "node:fs"
import { loadStack, stateFile } from "./stack"

// Stops exactly the processes the setup started, nothing else.
export default async function globalTeardown() {
  if (!existsSync(stateFile)) return
  for (const pid of loadStack().pids.reverse()) {
    try {
      process.kill(pid)
    } catch {
      // Already gone.
    }
  }
}
