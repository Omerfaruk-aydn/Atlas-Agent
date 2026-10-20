import { AtlasWordmark } from "../../atlas-wordmark"

export function NewSessionWordmark() {
  return (
    <div data-component="new-session-wordmark" aria-hidden="true" class="pointer-events-none mx-auto w-full max-w-[720px]">
      <div data-slot="wordmark-reveal" class="relative mx-auto w-4/5">
        <AtlasWordmark />
      </div>
    </div>
  )
}
