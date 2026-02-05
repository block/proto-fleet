/* eslint-disable playwright/expect-expect */
import { test } from "../fixtures/pageFixtures";

test.describe("Mining pools", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Check pool errors", async ({}) => {
    await test.step("Go to mining pool page", async () => {
      // click 'Settings' button
      // click View mining pools
    });
  });

  test("Set up backup pools", async ({}) => {
    await test.step("Validate current default pool", async () => {
      // click 'Mining Pool' button
      // validate popover visible - data-testid="pool-info-popover"
      // validate title in popover - 'Mining pool' (add same methods and validateTitleInModal)
      // text 'Connected' in popover
      // text 'Default Pool' in popover
      // default url in popover
      // click button in popover 'View mining pools'
    });
  });
});
