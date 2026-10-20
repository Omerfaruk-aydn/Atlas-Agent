// The slice of Atlas Agent's server SDK the copied request docks call. The
// Atlas session screen installs the implementation for its session.
import type { FormAnswer } from "@atlas/client/promise"
import type { ServerScope } from "@/runtime/server/scope"

export type ServerSDK = {
  scope: ServerScope
  api: {
    session: {
      form: {
        reply: (input: { sessionID: string; formID: string; answer: FormAnswer }) => Promise<unknown>
        cancel: (input: { sessionID: string; formID: string }) => Promise<unknown>
      }
    }
  }
}

let current: ServerSDK = {
  scope: "atlas" as ServerScope,
  api: { session: { form: { reply: () => Promise.reject(new Error("No session")), cancel: () => Promise.reject(new Error("No session")) } } },
}

export function setServerSDK(sdk: ServerSDK) {
  current = sdk
}

export function useServerSDK(): ServerSDK {
  return current
}
