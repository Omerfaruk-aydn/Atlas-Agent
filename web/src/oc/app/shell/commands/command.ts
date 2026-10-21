// Atlas Agent's command registry, reduced to what the copied views call.
// Atlas binds its shortcuts in the titlebar and screens directly.
const mac = typeof navigator === "object" && /(Mac|iPod|iPhone|iPad)/.test(navigator.platform)

export type CommandOption = { id: string; title: string; description?: string; slash?: string; disabled?: boolean; slashArguments?: boolean }

export function formatKeybind(key: string, _t?: unknown) {
  if (key === "mod") return mac ? "⌘" : "Ctrl"
  if (key === "esc") return "Esc"
  return key
}

export function useCommand() {
  return {
    register: (..._args: unknown[]) => {},
    keybind: (_id: string) => "",
    keybindParts: (_id: string): string[] => [],
    trigger: (_id: string, ..._args: unknown[]) => {},
    options: [] as CommandOption[],
  }
}
