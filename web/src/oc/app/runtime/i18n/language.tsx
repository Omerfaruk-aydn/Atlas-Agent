// Atlas Agent's useLanguage, backed by the Atlas language module.
import { useLanguage as base } from "../../../language"

export type Locale = "en" | "tr"

export function useLanguage() {
  return { ...base(), direction: (): "ltr" | "rtl" => "ltr" }
}
