import { expect, test, type Page } from "@playwright/test"
import { mkdirSync } from "node:fs"
import { join } from "node:path"
import { launchURL, loadStack, repo } from "./stack"

// Screenshots of the real UI on the real stack, light and dark, from phone
// to desktop width. Each capture also asserts the page does not scroll
// sideways: only code blocks and diffs may.

const stack = loadStack()
const out = process.env.ATLAS_SCREENSHOTS ?? join(repo, "dist", "dev", "web-ui-screenshots")
const widths = [360, 390, 768, 1280, 1440]
const permission = (page: Page) => page.locator("[data-component='dock-prompt'][data-kind='permission']")

async function noPageOverflow(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow, "the page scrolls horizontally").toBeLessThanOrEqual(0)
}

test("screenshots", async ({ browser }) => {
  test.setTimeout(240_000)
  mkdirSync(out, { recursive: true })
  const setup = await browser.newPage({ viewport: { width: 1280, height: 800 } })
  await setup.goto(await launchURL(stack))
  await expect(setup.locator("[data-slot='home-panel']")).toBeVisible()
  await setup.locator("[data-slot='home-panel'] [data-action='home-new-session']").click()
  await expect(setup).toHaveURL(/[?&]draft=/)
  const box = setup.locator("[data-component='composer'] [role='textbox']")
  for (const prompt of ["Explain `main.go:3` with a table and code [[big]]", "Run the build [[bash]]", "[[write docs/notes.md]]"]) {
    await box.fill(prompt)
    await box.press("Enter")
    // The prompt box goes away while a permission request is pending.
    if (prompt.includes("write")) break
    await expect(box).toHaveText("")
    // The send button turns into Stop while the run is in flight.
    await expect(setup.getByRole("button", { name: "Stop" })).toBeHidden({ timeout: 30_000 })
  }
  // Leave the permission request pending so the screenshots show it.
  await expect(permission(setup)).toBeVisible()
  await expect(setup).toHaveURL(/[?&]s=/)
  const url = setup.url()

  for (const scheme of ["light", "dark"] as const) {
    for (const width of widths) {
      const ctx = await browser.newContext({ viewport: { width, height: width < 500 ? 780 : 860 }, colorScheme: scheme })
      const page = await ctx.newPage()
      await page.goto(await launchURL(stack))
      await page.goto(url)
      await expect(permission(page)).toBeVisible()
      await page.waitForTimeout(400)
      await noPageOverflow(page)
      await page.screenshot({ path: join(out, `session-${scheme}-${width}.png`) })

      // The review panel's toggle is not in the phone-width header.
      if (width === 1280) {
        const toggle = page.locator("[aria-controls='review-panel']")
        await toggle.click()
        await expect(toggle).toHaveAttribute("aria-expanded", "true")
        await page.waitForTimeout(300)
        await noPageOverflow(page)
        await page.screenshot({ path: join(out, `review-${scheme}-${width}.png`) })
        await toggle.click()
        await expect(toggle).toHaveAttribute("aria-expanded", "false")
      }
      if (width === 360 || width === 1440) {
        // The tab strip folds into a menu on a phone, so go by address.
        await page.goto(new URL(url).origin + "/")
        await expect(page.locator("[data-slot='home-panel']")).toBeVisible()
        await page.waitForTimeout(300)
        await noPageOverflow(page)
        await page.screenshot({ path: join(out, `home-${scheme}-${width}.png`) })
        await page.goto(new URL(url).origin + "/?view=settings")
        await expect(page.locator("[data-testid='settings-screen']")).toBeVisible()
        await page.waitForTimeout(300)
        await noPageOverflow(page)
        await page.screenshot({ path: join(out, `settings-${scheme}-${width}.png`) })
      }
      await ctx.close()
    }
  }
  // Release the pending request so later runs start clean.
  await permission(setup).getByRole("button", { name: "Deny" }).click()
  await setup.close()
})
