import { expect, test } from "@playwright/test";

test.describe("Container Pools", () => {
  // The regular suite uses an MDK v1 rig; these tests require fake-ct1.
  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(!process.env.CT1_BASE_URL, "Requires a ProtoOS server pointed at fake-ct1 (CT1_BASE_URL)");
  test.use({ baseURL: process.env.CT1_BASE_URL });

  test("shows primary and backup history, changes ranges, and opens configuration", async ({ page }) => {
    await page.goto("/pools");
    const primary = page.getByRole("region", { name: "Primary", exact: true });
    const backup = page.getByRole("region", { name: "Backup 1", exact: true });
    const unused = page.getByRole("region", { name: "Backup 2", exact: true });
    await expect(primary.getByText("Active", { exact: true })).toBeVisible();
    await expect(primary.getByText("Worker: fake-ct1", { exact: true })).toBeVisible();
    await expect(primary.getByText("141.1K", { exact: true })).toBeVisible();
    await expect(backup.getByText("Standby", { exact: true })).toBeVisible();
    await expect(backup.getByText("90.0%", { exact: true })).toBeVisible();
    await expect(unused.getByText("0", { exact: true })).toHaveCount(2);
    await expect(page.getByText("Share history unavailable.")).toHaveCount(0);

    const historyRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname.endsWith("/telemetry/shares/history") && url.searchParams.get("poolId") === "1";
    });
    await page.getByRole("button", { name: "48h", exact: true }).click();
    const query = new URL((await historyRequest).url()).searchParams;
    expect(Date.parse(query.get("range.end")!) - Date.parse(query.get("range.start")!)).toBe(48 * 3_600_000);
    await expect(page.getByRole("button", { name: "48h", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect(primary.getByText("282.2K", { exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.getByRole("link", { name: "Configure pools" }).click();
    await expect(page).toHaveURL(/\/settings\/mining-pools$/);
  });

  test("shows one empty state when no pools are configured", async ({ page }) => {
    await page.route("**/api/v2/pools", async (route) => {
      await route.fulfill({ json: { pools: [] } });
    });
    await page.goto("/pools");
    await expect(page.getByText("No pools configured. Configure a pool to start mining.")).toBeVisible();
    await expect(page.getByRole("link", { name: "Configure pools" })).toHaveCount(1);
    await expect(page.getByText("No mining pools configured.", { exact: true })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Add mining pools" })).toHaveCount(0);
  });

  test("keeps other pools usable when one history request fails", async ({ page }) => {
    await page.route("**/api/v2/telemetry/shares/history?**", async (route) => {
      if (new URL(route.request().url()).searchParams.get("poolId") === "2") {
        await route.fulfill({ status: 503, contentType: "application/json", body: '{"message":"Unavailable"}' });
      } else {
        await route.continue();
      }
    });
    await page.goto("/pools");
    await expect(page.getByRole("region", { name: "Backup 1" })).toContainText("Share history unavailable.");
    await expect(page.getByRole("region", { name: "Primary", exact: true })).toContainText("98.0%");
    await expect(page.getByRole("region", { name: "Backup 2" }).getByText("0", { exact: true })).toHaveCount(2);
    await expect(page.getByRole("link", { name: "Configure pools" })).toBeVisible();
  });
});
