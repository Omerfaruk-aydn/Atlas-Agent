// The web build of Atlas Agent's platform hook: Atlas only runs in a browser,
// and persisted UI state uses localStorage (Atlas Agent's web default).
import type { AsyncStorage, SyncStorage } from "@solid-primitives/storage"

export type Platform = {
  platform: "web" | "desktop"
  os?: "macos" | "windows" | "linux"
  windowID?: string
  windowFullscreen?: () => boolean
  webviewZoom?: () => number
  storage?: (name?: string) => SyncStorage | AsyncStorage
  draftStore?: AsyncStorage
  openLink: (url: string) => void
  openExternal: (url: string) => void
}

const platform: Platform = {
  platform: "web",
  openLink: (url) => void window.open(url, "_blank", "noopener,noreferrer"),
  openExternal: (url) => void window.open(url, "_blank", "noopener,noreferrer"),
}

export function usePlatform(): Platform {
  return platform
}
