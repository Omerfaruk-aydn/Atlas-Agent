// The design kit's typefaces by the family name a design uses: the name the
// bundled @font-face declares and what to fall back to. Pure, so text
// measurement and tests can use it; fonts.ts loads the files.

export interface KitFont {
  /** The name the bundled @font-face declares. */
  family: string;
  generic: "sans-serif" | "serif" | "cursive";
}

const v = (name: string, generic: KitFont["generic"] = "sans-serif"): KitFont => ({ family: `${name} Variable`, generic });

export const KIT_FONT_NAMES: Record<string, KitFont> = {
  "Albert Sans": v("Albert Sans"),
  "DM Sans": v("DM Sans"),
  Figtree: v("Figtree"),
  Fraunces: v("Fraunces", "serif"),
  Fustat: v("Fustat"),
  Geist: v("Geist"),
  Montserrat: v("Montserrat"),
  Outfit: v("Outfit"),
  "Playwrite US Trad": v("Playwrite US Trad", "cursive"),
  Roboto: v("Roboto"),
  Poppins: { family: "Poppins", generic: "sans-serif" },
};

/** The bundled families, for the font picker. */
export const KIT_FAMILIES = Object.keys(KIT_FONT_NAMES).sort();
