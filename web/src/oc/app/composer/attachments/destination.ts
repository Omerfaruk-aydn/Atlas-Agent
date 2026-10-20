import type { Accessor } from "solid-js"
import type { ComposerControls } from "../adapter"

// Where a prompt is headed: the model that reads it and the server that runs
// its tools. Atlas sends attachments inline with the prompt, so nothing is
// uploaded ahead of time.
export type AttachmentDestination = {
  input: { image: boolean; pdf: boolean }
  local: boolean
  upload: (file: File, report: (loaded: number) => void, signal: AbortSignal) => Promise<string>
}

export function useAttachmentDestination(controls: Accessor<ComposerControls>) {
  return (): AttachmentDestination => ({
    input: controls().model.selection.current()?.capabilities.input ?? { image: true, pdf: false },
    local: false,
    upload: () => Promise.reject(new Error("Uploads are not supported")),
  })
}
