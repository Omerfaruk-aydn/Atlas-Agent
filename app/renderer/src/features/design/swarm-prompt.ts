// A design swarm: several agents each design the same brief on a page of their own, and the lead compares them.

export const SWARM_MIN = 2;
export const SWARM_MAX = 4;

/** A distinct direction per variant, so the results differ in more than details. The theme is shared, so they differ in composition. */
export const DIRECTIONS = [
  "clean and minimal: generous whitespace, few sections, the accent used once or twice",
  "bold and expressive: display-size type, a full-bleed accent band, confident contrast",
  "dense and information-rich: compact spacing, more sections, clear hierarchy",
  "friendly and visual: large imagery, rounded cards, warm approachable copy",
];

export interface SwarmRequest {
  /** The design file the variants go into. */
  file: string;
  brief: string;
  /** How many variants: kept within SWARM_MIN..SWARM_MAX. */
  count: number;
  /** Configured subagents (one per variant, in order, cycling) so each can run on its own model; empty means the default agent. */
  agents: string[];
}

export function clampCount(n: number): number {
  return Math.min(SWARM_MAX, Math.max(SWARM_MIN, Number.isFinite(n) ? Math.round(n) : SWARM_MIN));
}

/** Names as a person types them: comma or space separated, no duplicates of nothing. */
export function parseAgents(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter((s) => /^[\w.-]{1,64}$/.test(s))
    .slice(0, SWARM_MAX);
}

export function variantName(i: number): string {
  return `Variant ${String.fromCharCode(65 + i)}`;
}

/** The message that sends the lead agent off to run the swarm. */
export function swarmPrompt(req: SwarmRequest): string {
  const n = clampCount(req.count);
  const brief = req.brief.trim();
  const lines: string[] = [];
  lines.push(`Run a design swarm in the design file "${req.file}" for this brief:`, "", brief, "");
  lines.push(`Make ${n} competing variants, each on its own page of "${req.file}".`, "");
  lines.push(
    "1. Pick the kit theme that fits the brief (design_canvas action themes; choose by the brief, not the first one) and, if the brief names a colour, an accent.",
    "   In one design_canvas apply, set it (set_theme) and add these pages (add_page) before anything else:",
  );
  for (let i = 0; i < n; i++) lines.push(`   - ${variantName(i)}: ${DIRECTIONS[i]}`);
  lines.push("");
  lines.push(
    `2. Start ${n} sub-agents in parallel with the agent tool, all in one step and each with run_in_background: true, one per page.`,
    "   Each prompt must contain: the brief, the file name, its page name and direction, and these rules:",
    "   - only touch its own page: pass page on every design_canvas call, never edit another page;",
    "   - never add or rename pages; put the whole design in ONE top-level frame on its page, and revise that frame instead of adding a second one;",
    "   - never call set_theme, set_modes or change shared styles and variables: the theme is set for every variant;",
    "   - build from the design kit: one column frame for the screen, then insert blocks (action blocks lists them and their slots) and icons;",
    "     write every slot in the brief's own words, never keep sample copy, and add custom layers only with the theme's variables and text styles;",
    "   - when done, run lint on its page and fix every error, export its page and look at it, then reply with a two-line summary and the lint score.",
  );
  if (req.agents.length > 0) {
    lines.push(`   Run them as these configured subagents, in order (use agent_name): ${req.agents.join(", ")}.${req.agents.length < n ? " Cycle the list when there are more variants than names." : ""}`);
  }
  lines.push("");
  lines.push(
    "3. Read each result with agent_result. If one failed, retry only that one.",
    "4. Lint and export every page and compare them against the brief on hierarchy, spacing, contrast, copy and consistency.",
    "5. Report which variant is strongest and why, what to take from the others, and keep all variants. Do not delete pages; ask me which to keep or merge.",
  );
  return lines.join("\n");
}
