// Task checks for event-landing-open. The prompt leaves names and wiring to
// the builder, so these find controls by role and a forgiving name pattern and
// judge behaviour, not wording or markup.

export const threshold = 80;

const speakers = ["Amara Okafor", "Jonas Lindqvist", "Mei Tanaka", "Diego Ramírez", "Fatima Zahra Bennani", "Liam O'Connor"];
const day1 = "Opening keynote";
const day2 = "Postmortems without blame";
const day3 = "Closing panel";

// A phrase that tells the visitor registration worked. The page may contain
// words like "registration" or "confirm" before anything is submitted, so a
// result is judged by whether such a phrase appears that was not there before.
const done = /you(?:'|’)re (?:registered|in|all set|booked)|you are (?:registered|in|all set)|registration (?:complete|confirmed|successful|received)|(?:successfully|now) registered|thank you|thanks for|see you in|booking confirmed/i;

// Non-breaking spaces and curly quotes are common in polished copy.
const text = async (page) => (await page.locator("body").innerText()).replace(/ /g, " ").replace(/[’‘]/g, "'");

async function showing(page, needle) {
  return page.getByText(needle).first().isVisible().catch(() => false);
}

async function fields(page) {
  const name = page.getByRole("textbox", { name: /name/i }).first();
  const email = page.getByRole("textbox", { name: /e-?mail/i }).first();
  const submit = page.getByRole("button", { name: /regist|sign ?up|submit|book|reserve|buy|complete|confirm/i }).last();
  return { name, email, submit };
}

// Picks a ticket the way the page offers it: a select, a radio group or a
// card button. A visitor has to choose one, so tests that register do too.
async function chooseTicket(page, label, index) {
  const select = page.getByRole("combobox", { name: /ticket|type|pass/i }).first();
  if (await select.isVisible().catch(() => false)) {
    await select.selectOption({ label: new RegExp(label, "i") }).catch(() => select.selectOption({ index }));
    return;
  }
  const radio = page.getByRole("radio", { name: new RegExp(label, "i") }).first();
  if ((await radio.count()) > 0) {
    await radio.check({ force: true });
    return;
  }
  await page.getByRole("button", { name: new RegExp(`(?:choose|select|get|buy|pick)?.*${label}`, "i") }).first().click();
}

const chooseTeam = (page) => chooseTicket(page, "Team", 2);
const chooseStandard = (page) => chooseTicket(page, "Standard", 0);

function teamSize(page) {
  return page
    .getByRole("spinbutton")
    .or(page.getByRole("textbox", { name: /team size|people|attendees|seats|members|group size/i }))
    .first();
}

export const checks = [
  {
    name: "one h1 naming the event",
    points: 2,
    run: async ({ open }) => {
      const page = await open();
      const headings = await page.getByRole("heading", { level: 1 }).allTextContents();
      return headings.length === 1 && /Northwind\s+Dev\s+Summit/i.test(headings[0].replace(/ /g, " "));
    },
  },
  {
    name: "all speakers and ticket prices appear",
    points: 5,
    run: async ({ open }) => {
      const page = await open();
      const body = await text(page);
      const names = speakers.filter((s) => body.includes(s)).length / speakers.length;
      const prices = ["299", "99", "249"].filter((p) => body.includes(p)).length / 3;
      return (names + prices) / 2;
    },
  },
  {
    name: "the schedule is browsable one day at a time",
    points: 8,
    run: async ({ open }) => {
      const page = await open();
      const tab2 = page.getByRole("tab", { name: /day 2/i }).first();
      const control = (await tab2.isVisible().catch(() => false)) ? tab2 : page.getByRole("button", { name: /day 2/i }).first();
      if (!(await control.isVisible().catch(() => false))) return { score: 0, note: "no control for Day 2" };
      let score = 0.2;
      if (await showing(page, day1)) score += 0.2;
      await control.click();
      if (await showing(page, day2)) score += 0.3;
      if (!(await showing(page, day1))) score += 0.3;
      return score;
    },
  },
  {
    name: "schedule tabs are keyboard-operable with arrow keys",
    points: 5,
    run: async ({ open }) => {
      const page = await open();
      const tab1 = page.getByRole("tab", { name: /day 1/i }).first();
      if (!(await tab1.isVisible().catch(() => false))) return { score: 0, note: "the days are not exposed as tabs" };
      await tab1.focus();
      await page.keyboard.press("ArrowRight");
      const moved = await showing(page, day2);
      await page.keyboard.press("End");
      const end = await showing(page, day3);
      return (moved ? 0.6 : 0) + (end ? 0.4 : 0);
    },
  },
  {
    name: "navigation works on a phone (links reachable, or a menu that opens and closes)",
    points: 6,
    run: async ({ open }) => {
      const page = await open({ width: 375, height: 812 });
      const link = page.getByRole("link", { name: /speakers/i }).first();
      const menu = page.getByRole("button", { name: /menu|navigation|nav/i }).first();
      if (!(await menu.isVisible().catch(() => false))) {
        // No menu: the links must simply be reachable and work.
        if (!(await link.isVisible().catch(() => false))) return { score: 0, note: "no visible navigation and no menu button at 375px" };
        await link.click();
        return { score: 1 };
      }
      let score = 0.3;
      await menu.click();
      if (await link.isVisible()) score += 0.4;
      await page.keyboard.press("Escape");
      if (!(await link.isVisible())) score += 0.3;
      return score;
    },
  },
  {
    name: "wrong input is explained and nothing is confirmed",
    points: 10,
    run: async ({ open }) => {
      const page = await open();
      const { name, email, submit } = await fields(page);
      const before = await text(page);
      await submit.click();
      let score = 0;
      if (!done.test(await text(page)) || done.test(before)) score += 0.3;
      if ((await name.getAttribute("aria-invalid")) === "true" || (await email.getAttribute("aria-invalid")) === "true") score += 0.3;
      const described = await email.evaluate((el) => {
        const ids = (el.getAttribute("aria-describedby") || "").split(/\s+/).filter(Boolean);
        return ids.some((id) => (document.getElementById(id)?.textContent ?? "").trim().length > 0);
      });
      if (described) score += 0.2;
      const focused = await page.evaluate(() => document.activeElement?.tagName);
      if (focused === "INPUT" || focused === "SELECT" || focused === "TEXTAREA") score += 0.2;
      await name.fill("Ada Lovelace");
      await email.fill("not-an-email");
      await submit.click();
      if (done.test(await text(page))) score = Math.max(0, score - 0.4);
      return score;
    },
  },
  {
    name: "a valid registration is confirmed (naming the email earns the last quarter)",
    points: 8,
    run: async ({ open }) => {
      const page = await open();
      const { name, email, submit } = await fields(page);
      await chooseStandard(page);
      await name.fill("Ada Lovelace");
      await email.fill("ada@example.com");
      await submit.click();
      const body = await text(page);
      if (!done.test(body)) return { score: 0, note: "no confirmation after a valid submission" };
      return body.includes("ada@example.com") ? 1 : 0.75;
    },
  },
  {
    name: "the amount owed follows the ticket and the size of a team",
    points: 10,
    run: async ({ open }) => {
      const page = await open();
      await chooseTeam(page);
      const size = teamSize(page);
      if (!(await size.isVisible().catch(() => false))) return { score: 0.1, note: "no team size field after choosing Team" };
      await size.fill("3");
      let score = 0.3;
      const total = async () => (await text(page)).replace(/[,\s]/g, "");
      if ((await total()).includes("747")) score += 0.4;
      await size.fill("10");
      if ((await total()).includes("2490")) score += 0.3;
      return score;
    },
  },
  {
    name: "a team size outside 3-20 is refused",
    points: 4,
    run: async ({ open }) => {
      const page = await open();
      const { name, email, submit } = await fields(page);
      await chooseTeam(page);
      const size = teamSize(page);
      if (!(await size.isVisible().catch(() => false))) return 0;
      await name.fill("Ada Lovelace");
      await email.fill("ada@example.com");
      await size.fill("2");
      await submit.click();
      return !done.test(await text(page));
    },
  },
];
