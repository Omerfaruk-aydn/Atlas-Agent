// Task checks for event-landing, run by bench/judges/site/judge.mjs.

export const threshold = 85;

const speakers = ["Amara Okafor", "Jonas Lindqvist", "Mei Tanaka", "Diego Ramírez", "Fatima Zahra Bennani", "Liam O'Connor"];

async function form(open, options) {
  const page = await open(options);
  return {
    page,
    // By role and accessible name, which ignores a decorative "*" in an
    // aria-hidden span; getByLabel would compare the raw label text.
    name: page.getByRole("textbox", { name: "Full name", exact: true }),
    email: page.getByRole("textbox", { name: "Email", exact: true }),
    ticket: page.getByRole("combobox", { name: "Ticket", exact: true }),
    teamSize: page.getByRole("spinbutton", { name: "Team size", exact: true }),
    submit: page.getByRole("button", { name: "Register", exact: true }),
  };
}

async function selectedTab(page) {
  return page.getByRole("tab", { selected: true }).textContent().then((t) => t.trim());
}

async function focusedName(page) {
  return page.evaluate(() => document.activeElement?.textContent?.trim() ?? "");
}

async function describedError(field) {
  return field.evaluate((el) => {
    const ids = (el.getAttribute("aria-describedby") || "").split(/\s+/).filter(Boolean);
    return ids
      .map((id) => document.getElementById(id))
      .filter((node) => node && node.offsetParent !== null)
      .map((node) => node.textContent.trim())
      .join(" ");
  });
}

export const checks = [
  {
    name: "event name is the only h1",
    points: 2,
    run: async ({ open }) => {
      const page = await open();
      const headings = await page.getByRole("heading", { level: 1 }).allTextContents();
      return headings.length === 1 && headings[0].includes("Northwind Dev Summit 2027");
    },
  },
  {
    name: "every speaker has an h3 card with role and company",
    points: 5,
    run: async ({ open }) => {
      const page = await open();
      let found = 0;
      for (const name of speakers) {
        if (await page.getByRole("heading", { level: 3, name, exact: true }).isVisible()) found++;
      }
      const text = await page.locator("body").innerText();
      const details = ["Kestrel Systems", "Test Architect", "Atlas Security", "Copperleaf"].every((s) => text.includes(s));
      return (found / speakers.length) * (details ? 1 : 0.5);
    },
  },
  {
    name: "schedule tabs follow the ARIA tabs structure",
    points: 5,
    run: async ({ open }) => {
      const page = await open();
      const tabs = page.getByRole("tablist").getByRole("tab");
      const names = (await tabs.allTextContents()).map((t) => t.trim());
      if (names.join("|") !== "Day 1|Day 2|Day 3") return { score: 0, note: `tabs are ${JSON.stringify(names)}` };
      if ((await selectedTab(page)) !== "Day 1") return { score: 0.4, note: "Day 1 is not selected at first" };
      const controls = await tabs.first().getAttribute("aria-controls");
      const panel = controls && page.locator(`[id="${controls}"]`);
      if (!panel || (await panel.getAttribute("role")) !== "tabpanel") return { score: 0.6, note: "aria-controls does not point at a tabpanel" };
      return (await page.getByRole("tabpanel").count()) === 1 ? true : { score: 0.8, note: "more than one tab panel is visible" };
    },
  },
  {
    name: "clicking a tab shows only that day",
    points: 5,
    run: async ({ open }) => {
      const page = await open();
      await page.getByRole("tab", { name: "Day 2", exact: true }).click();
      const day2 = await page.getByText("Postmortems without blame").isVisible();
      const day1 = await page.getByText("Opening keynote: Software that lasts").isVisible();
      const time = (await page.getByRole("tabpanel").innerText()).includes("09:30");
      return day2 && !day1 && time;
    },
  },
  {
    name: "arrow keys, Home and End move between tabs",
    points: 5,
    run: async ({ open }) => {
      const page = await open();
      await page.getByRole("tab", { name: "Day 1", exact: true }).focus();
      const steps = [
        ["ArrowRight", "Day 2"],
        ["ArrowRight", "Day 3"],
        ["ArrowRight", "Day 1"],
        ["ArrowLeft", "Day 3"],
        ["Home", "Day 1"],
        ["End", "Day 3"],
      ];
      let good = 0;
      for (const [key, want] of steps) {
        await page.keyboard.press(key);
        if ((await selectedTab(page)) === want && (await focusedName(page)) === want) good++;
      }
      return good / steps.length;
    },
  },
  {
    name: "phone menu button toggles the links and closes on Escape",
    points: 8,
    run: async ({ open }) => {
      const page = await open({ width: 375, height: 812 });
      const menu = page.getByRole("button", { name: "Menu", exact: true });
      const link = page.getByRole("link", { name: "Speakers", exact: true }).first();
      if (!(await menu.isVisible())) return { score: 0, note: "no visible Menu button at 375px" };
      let score = 0;
      if ((await menu.getAttribute("aria-expanded")) === "false" && !(await link.isVisible())) score += 0.25;
      await menu.click();
      if ((await menu.getAttribute("aria-expanded")) === "true" && (await link.isVisible())) score += 0.35;
      await link.focus();
      await page.keyboard.press("Escape");
      const closed = (await menu.getAttribute("aria-expanded")) === "false" && !(await link.isVisible());
      const focused = await menu.evaluate((el) => el === document.activeElement);
      if (closed) score += 0.2;
      if (focused) score += 0.2;
      return score;
    },
  },
  {
    name: "desktop shows the links and hides the Menu button",
    points: 2,
    run: async ({ open }) => {
      const page = await open({ width: 1024 });
      const menuVisible = await page.getByRole("button", { name: "Menu", exact: true }).isVisible();
      const links = ["Schedule", "Speakers", "Tickets", "Register"];
      let visible = 0;
      for (const name of links) {
        if (await page.getByRole("link", { name, exact: true }).first().isVisible()) visible++;
      }
      return visible === links.length && !menuVisible;
    },
  },
  {
    name: "empty submit marks fields invalid, explains and focuses the first",
    points: 8,
    run: async ({ open }) => {
      const { page, name, email, submit } = await form(open);
      await submit.click();
      let score = 0;
      const status = await page.getByRole("status").filter({ hasText: "registered" }).count();
      if (status === 0) score += 0.2;
      if ((await name.getAttribute("aria-invalid")) === "true" && (await email.getAttribute("aria-invalid")) === "true") score += 0.3;
      if ((await describedError(name)) && (await describedError(email))) score += 0.3;
      if (await name.evaluate((el) => el === document.activeElement)) score += 0.2;
      return score;
    },
  },
  {
    name: "a malformed email is rejected",
    points: 3,
    run: async ({ open }) => {
      const { page, name, email, submit } = await form(open);
      await name.fill("Ada Lovelace");
      await email.fill("ada@");
      await submit.click();
      const invalid = (await email.getAttribute("aria-invalid")) === "true";
      const status = await page.getByRole("status").filter({ hasText: "registered" }).count();
      return invalid && status === 0 && (await email.evaluate((el) => el === document.activeElement));
    },
  },
  {
    name: "team size appears only for Team and is limited to 3-20",
    points: 4,
    run: async ({ open }) => {
      const { page, name, email, ticket, teamSize, submit } = await form(open);
      let score = 0;
      await ticket.selectOption({ label: "Standard" });
      if (!(await teamSize.isVisible())) score += 0.3;
      await ticket.selectOption({ label: "Team" });
      if (await teamSize.isVisible()) score += 0.3;
      await name.fill("Ada Lovelace");
      await email.fill("ada@example.com");
      await teamSize.fill("2");
      await submit.click();
      const rejected = (await teamSize.getAttribute("aria-invalid")) === "true" && (await page.getByRole("status").filter({ hasText: "registered" }).count()) === 0;
      if (rejected) score += 0.4;
      return score;
    },
  },
  {
    name: "a valid registration confirms with the email",
    points: 5,
    run: async ({ open }) => {
      const { page, name, email, ticket, submit } = await form(open);
      await name.fill("Ada Lovelace");
      await email.fill("ada@example.com");
      await ticket.selectOption({ label: "Student" });
      await submit.click();
      const status = page.getByRole("status").filter({ hasText: "You're registered" });
      return (await status.count()) > 0 && (await status.first().innerText()).includes("ada@example.com");
    },
  },
  {
    name: "the total follows ticket and team size",
    points: 4,
    run: async ({ open }) => {
      const { page, ticket, teamSize } = await form(open);
      let score = 0;
      await ticket.selectOption({ label: "Standard" });
      if (await page.getByText("Total: €299").isVisible()) score += 0.3;
      await ticket.selectOption({ label: "Team" });
      await teamSize.fill("3");
      if (await page.getByText("Total: €747").isVisible()) score += 0.4;
      await teamSize.fill("10");
      if (await page.getByText("Total: €2490").or(page.getByText("Total: €2,490")).first().isVisible()) score += 0.3;
      return score;
    },
  },
  {
    name: "choosing a ticket fills the form and focuses Full name",
    points: 4,
    run: async ({ open }) => {
      const { page, name, ticket } = await form(open);
      await page.getByRole("button", { name: "Choose Student", exact: true }).click();
      const value = await ticket.evaluate((el) => el.options[el.selectedIndex]?.text.trim());
      const focused = await name.evaluate((el) => el === document.activeElement);
      return (value === "Student" ? 0.6 : 0) + (focused ? 0.4 : 0);
    },
  },
  {
    name: "ticket prices are shown in euros",
    points: 2,
    run: async ({ open }) => {
      const page = await open();
      const text = await page.locator("body").innerText();
      return ["€299", "€99", "€249"].every((p) => text.includes(p));
    },
  },
];
