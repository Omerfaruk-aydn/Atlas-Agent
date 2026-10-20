// The model selection the copied composer reads: what is chosen and what it
// can take as input. Atlas fills it from its model catalog (../../../composer).
import type { Accessor } from "solid-js"

export type ModelChoice = {
  id: string
  name: string
  provider: { id: string; canonical?: string; name: string }
  capabilities: { input: { image: boolean; pdf: boolean } }
  variants?: string[]
}

export type ModelSelection = {
  current: Accessor<ModelChoice | undefined>
  variant: { current: Accessor<string | undefined>; list: Accessor<string[]>; set: (value: string | undefined) => void }
}
