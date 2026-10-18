// Scores the visual design of a site with a vision model, for judge.mjs.
//
// Screenshots are written to a temporary directory outside the workspace and a
// headless `atlas run` call reads them and answers with one JSON object of
// 1-5 ratings. Ratings are a calibrated rubric rather than a taste test: the
// prompt tells the rater that 3 is a competent but generic page and that 5 is
// rare, because a lenient rater would put every site at the ceiling.
//
// This step costs model tokens, so judge.mjs runs it only when BENCH_DESIGN=1.
// BENCH_DESIGN_MODEL picks the rater (default claude/claude-sonnet-5-5).

import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

export const criteria = {
  hierarchy: "Visual hierarchy: the eye knows where to start and what matters most; headings, sizes and weights guide reading.",
  spacing: "Spacing and alignment: consistent rhythm, generous but not wasteful whitespace, aligned edges, nothing cramped or floating.",
  typography: "Typography: readable sizes and line lengths, a considered pairing of weights, no more than two type voices.",
  color: "Colour: a restrained, coherent palette with a clear accent; both light and dark screenshots look designed, not merely inverted.",
  polish: "Polish and consistency: components share one visual language (radii, borders, shadows, button styles); the phone layout is composed, not just squeezed.",
};

const prompt = `You are a strict senior product designer reviewing a conference web page from screenshots.
Use your file viewing tool to look at each of these images in the current directory:
- desktop-light.png (1280px wide, light mode)
- desktop-dark.png (1280px wide, dark mode)
- phone-light.png (375px wide, light mode)

Rate five criteria from 1 to 5 (whole numbers):
${Object.entries(criteria).map(([key, text]) => `- ${key}: ${text}`).join("\n")}

Calibration: 1 = broken or unstyled; 2 = rough, obvious problems; 3 = competent but generic, like an unmodified template; 4 = clearly designed, with only small flaws; 5 = excellent, would pass a design critique at a good studio. Most pages deserve 2 or 3. Give 4 only for a page you could not easily improve in a few ways, and 5 almost never. Judge only what is visible in the screenshots, not the code.

After looking at the images, reply with ONLY one JSON object and nothing else, for example:
{"hierarchy":3,"spacing":3,"typography":3,"color":3,"polish":3,"notes":"one sentence naming the biggest weakness"}`;

// The rater is Atlas itself (BENCH_ATLAS names the binary, default "atlas"),
// so it uses the login the bench already runs with. Its data directory is
// private to the call, and --yolo only lets it read the screenshots it is
// pointed at in a throwaway directory.
function ask(cwd, model) {
  return new Promise((resolve, reject) => {
    const program = process.env.BENCH_ATLAS || "atlas";
    const args = ["--cwd", cwd, "--data-dir", path.join(cwd, ".state"), "--yolo", "run", "--model", model, "--role", "none", "--quiet", prompt];
    const child = spawn(program, args, { cwd });
    let out = "";
    let err = "";
    const timer = setTimeout(() => {
      child.kill();
      reject(new Error("the design rater timed out after 300 s"));
    }, 300_000);
    child.stdout.on("data", (chunk) => (out += chunk));
    child.stderr.on("data", (chunk) => (err += chunk));
    child.on("error", reject);
    child.on("close", (code) => {
      clearTimeout(timer);
      if (code !== 0) reject(new Error(`the design rater exited ${code}: ${err.trim().slice(0, 200)}`));
      else resolve(out);
    });
    child.stdin.end();
  });
}

// rate takes an open(options) function and returns {score: 0-1, note}.
export async function rate(open) {
  const dir = await mkdtemp(path.join(tmpdir(), "atlas-bench-design-"));
  try {
    const shots = [
      ["desktop-light.png", { width: 1280, height: 800, colorScheme: "light" }],
      ["desktop-dark.png", { width: 1280, height: 800, colorScheme: "dark" }],
      ["phone-light.png", { width: 375, height: 812, colorScheme: "light" }],
    ];
    for (const [file, options] of shots) {
      const page = await open(options);
      // Let entrance animations finish before looking.
      await page.waitForTimeout(400);
      // Models reject images over 8000px on a side, so a very long page is cut.
      const height = await page.evaluate(() => document.documentElement.scrollHeight);
      await page.screenshot({
        path: path.join(dir, file),
        fullPage: true,
        ...(height > 7000 ? { clip: { x: 0, y: 0, width: options.width, height: 7000 } } : {}),
      });
    }
    const model = process.env.BENCH_DESIGN_MODEL || "claude/claude-sonnet-5-5";
    const reply = await ask(dir, model);
    const match = reply.match(/\{[\s\S]*\}/);
    if (!match) throw new Error(`the design rater did not return JSON: ${reply.trim().slice(0, 120)}`);
    const rating = JSON.parse(match[0]);
    const values = Object.keys(criteria).map((key) => Number(rating[key]));
    if (values.some((v) => !Number.isFinite(v) || v < 1 || v > 5)) throw new Error(`invalid ratings: ${match[0].slice(0, 120)}`);
    const mean = values.reduce((a, b) => a + b, 0) / values.length;
    const detail = Object.keys(criteria).map((key, i) => `${key} ${values[i]}`).join(", ");
    // 1 maps to 0 and 5 to 1; a generic 3 is half marks.
    return { score: (mean - 1) / 4, note: `${detail}. ${String(rating.notes ?? "").slice(0, 160)}` };
  } finally {
    await rm(dir, { recursive: true, force: true }).catch(() => {});
  }
}
