import { Schema } from "effect"

export const ID = Schema.String.pipe(Schema.brand("Skill.ID"), Schema.annotate({ identifier: "Skill.ID" }))
export type ID = typeof ID.Type
export const Name = Schema.String.pipe(Schema.brand("Skill.Name"), Schema.annotate({ identifier: "Skill.Name" }))
export type Name = typeof Name.Type
