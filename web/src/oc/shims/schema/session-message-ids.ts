import { Schema } from "effect"

export const ID = Schema.String.pipe(Schema.brand("SessionMessage.ID"), Schema.annotate({ identifier: "SessionMessage.ID" }))
export type ID = typeof ID.Type
