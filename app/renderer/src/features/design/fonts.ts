// The typefaces the design kit's themes use, bundled with the app (all under
// the SIL Open Font License or Apache 2.0) so designs look the same on every
// machine: their CSS family names, what to fall back to, and the files an
// export embeds.

import "@fontsource-variable/albert-sans";
import "@fontsource-variable/dm-sans";
import "@fontsource-variable/figtree";
import "@fontsource-variable/fraunces";
import "@fontsource-variable/fustat";
import "@fontsource-variable/geist";
import "@fontsource-variable/montserrat";
import "@fontsource-variable/outfit";
import "@fontsource-variable/playwrite-us-trad";
import "@fontsource-variable/roboto";
import "@fontsource/poppins/400.css";
import "@fontsource/poppins/500.css";
import "@fontsource/poppins/600.css";
import "@fontsource/poppins/700.css";
import albertLatin from "@fontsource-variable/albert-sans/files/albert-sans-latin-wght-normal.woff2?url";
import albertLatinExt from "@fontsource-variable/albert-sans/files/albert-sans-latin-ext-wght-normal.woff2?url";
import dmLatin from "@fontsource-variable/dm-sans/files/dm-sans-latin-wght-normal.woff2?url";
import dmLatinExt from "@fontsource-variable/dm-sans/files/dm-sans-latin-ext-wght-normal.woff2?url";
import figtreeLatin from "@fontsource-variable/figtree/files/figtree-latin-wght-normal.woff2?url";
import figtreeLatinExt from "@fontsource-variable/figtree/files/figtree-latin-ext-wght-normal.woff2?url";
import frauncesLatin from "@fontsource-variable/fraunces/files/fraunces-latin-wght-normal.woff2?url";
import frauncesLatinExt from "@fontsource-variable/fraunces/files/fraunces-latin-ext-wght-normal.woff2?url";
import fustatLatin from "@fontsource-variable/fustat/files/fustat-latin-wght-normal.woff2?url";
import fustatLatinExt from "@fontsource-variable/fustat/files/fustat-latin-ext-wght-normal.woff2?url";
import geistLatin from "@fontsource-variable/geist/files/geist-latin-wght-normal.woff2?url";
import geistLatinExt from "@fontsource-variable/geist/files/geist-latin-ext-wght-normal.woff2?url";
import montserratLatin from "@fontsource-variable/montserrat/files/montserrat-latin-wght-normal.woff2?url";
import montserratLatinExt from "@fontsource-variable/montserrat/files/montserrat-latin-ext-wght-normal.woff2?url";
import outfitLatin from "@fontsource-variable/outfit/files/outfit-latin-wght-normal.woff2?url";
import outfitLatinExt from "@fontsource-variable/outfit/files/outfit-latin-ext-wght-normal.woff2?url";
import playwrite from "@fontsource-variable/playwrite-us-trad/files/playwrite-us-trad-fallback-wght-normal.woff2?url";
import poppins400 from "@fontsource/poppins/files/poppins-latin-400-normal.woff2?url";
import poppins500 from "@fontsource/poppins/files/poppins-latin-500-normal.woff2?url";
import poppins600 from "@fontsource/poppins/files/poppins-latin-600-normal.woff2?url";
import poppins700 from "@fontsource/poppins/files/poppins-latin-700-normal.woff2?url";
import robotoLatin from "@fontsource-variable/roboto/files/roboto-latin-wght-normal.woff2?url";
import robotoLatinExt from "@fontsource-variable/roboto/files/roboto-latin-ext-wght-normal.woff2?url";
import { KIT_FONT_NAMES, type KitFont } from "./kit-fonts";

export interface BundledFont extends KitFont {
  /** Files to embed in an export; a weight when the file holds only one. */
  files: { url: string; weight?: number }[];
}

const files = (name: string, ...urls: string[]): BundledFont => ({ ...KIT_FONT_NAMES[name]!, files: urls.map((url) => ({ url })) });

/** By the family name a design uses. Inter and JetBrains Mono are loaded by main.tsx and exporter.ts. */
export const KIT_FONTS: Record<string, BundledFont> = {
  "Albert Sans": files("Albert Sans", albertLatin, albertLatinExt),
  "DM Sans": files("DM Sans", dmLatin, dmLatinExt),
  Figtree: files("Figtree", figtreeLatin, figtreeLatinExt),
  Fraunces: files("Fraunces", frauncesLatin, frauncesLatinExt),
  Fustat: files("Fustat", fustatLatin, fustatLatinExt),
  Geist: files("Geist", geistLatin, geistLatinExt),
  Montserrat: files("Montserrat", montserratLatin, montserratLatinExt),
  Outfit: files("Outfit", outfitLatin, outfitLatinExt),
  "Playwrite US Trad": files("Playwrite US Trad", playwrite),
  Roboto: files("Roboto", robotoLatin, robotoLatinExt),
  Poppins: {
    ...KIT_FONT_NAMES.Poppins!,
    files: [
      { url: poppins400, weight: 400 },
      { url: poppins500, weight: 500 },
      { url: poppins600, weight: 600 },
      { url: poppins700, weight: 700 },
    ],
  },
};

let loading: Promise<void> | null = null;

/**
 * Loads every kit font now, so text is measured with the real face rather
 * than a fallback: canvas measurement does not wait for a font to arrive.
 * Resolves once they are in (or failed, which leaves the fallback).
 */
export function loadKitFonts(): Promise<void> {
  if (loading) return loading;
  if (typeof document === "undefined" || !document.fonts?.load) return (loading = Promise.resolve());
  const jobs: Promise<unknown>[] = [];
  for (const f of Object.values(KIT_FONTS)) for (const w of [400, 500, 600, 700]) jobs.push(document.fonts.load(`${w} 16px "${f.family}"`).catch(() => undefined));
  loading = Promise.all(jobs).then(() => undefined);
  return loading;
}
