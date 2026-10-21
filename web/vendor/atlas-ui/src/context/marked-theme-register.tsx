import { registerCustomTheme } from "@pierre/diffs"
import { AtlasTheme } from "./marked-theme"

let registered = false

export function registerAtlasTheme() {
  if (registered) return
  registered = true
  registerCustomTheme("Atlas Agent", () => Promise.resolve(AtlasTheme))
}
