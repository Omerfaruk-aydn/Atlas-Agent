// Types from Atlas Agent's extension SDK that copied modules mention.
export type FileContent = { type: "text" | "binary"; content: string; mimeType?: string; encoding?: string }
export type Links = Record<string, never>
export type MountedSession = { id: string }

import { createContext } from "solid-js"
/** Drawer navigation context the mobile panel drawer provides. */
export const DrawerContext = createContext<unknown>()
