// Composer contracts from Atlas Agent's composer/adapter.ts that the copied
// editor and interaction code use. Atlas implements them in ../../composer.
import type { Accessor } from "solid-js"
import type { ModelSelection } from "@/providers/models/selection"

export type ComposerControls = {
  agents: {
    available: { name: string; hidden?: boolean; mode: string }[]
    options: string[]
    current: string
    visible: boolean
    select: (name: string | undefined) => void
  }
  model: {
    selection: ModelSelection
    paid: boolean
    loading: boolean
  }
}

export type ComposerDelivery = "steer" | "queue"

// Contract between the composer and the session prompt queue.
export type ComposerQueue = {
  count: Accessor<number>
  undoing: Accessor<boolean>
  delivery: Accessor<ComposerDelivery>
  alternate: Accessor<ComposerDelivery | undefined>
  editing: Accessor<string | undefined>
  confirmEdit: (delivery: ComposerDelivery) => void
  cancelEdit: () => void
  editFirst: () => boolean
}
