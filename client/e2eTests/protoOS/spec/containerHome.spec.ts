import { expect, test } from "@playwright/test";

// Run against an isolated fake-ct1-backed ProtoOS server, not the rig fixture
// used by the existing onboarding/settings suite.
test.describe("Container Home", () => {
  // The default suite targets a rig; this opt-in fixture requires CT1 telemetry.
  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(!process.env.CT1_BASE_URL, "Requires a ProtoOS server pointed at fake-ct1 (CT1_BASE_URL)");
  test.use({ baseURL: process.env.CT1_BASE_URL });

  test("Overview, metric modes and durations use aggregate requests", async ({ page }) => {
    const histories: URL[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.pathname.endsWith("/telemetry/timeseries")) histories.push(url);
    });
    await page.goto("/");
    const navigation = page.getByRole("navigation", { name: "Container Home" });
    await expect(navigation.getByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page");
    await expect(page.getByRole("heading", { name: "Container overview" })).toBeAttached();
    await expect(page.getByRole("link", { name: "View Hashrate" })).toBeVisible();
    await expect(page.locator(".recharts-line-curve")).toHaveCount(1);
    await page.getByRole("button", { name: "48h" }).click();
    await expect(page.getByRole("button", { name: "48h" })).toHaveAttribute("aria-pressed", "true");
    await navigation.getByRole("link", { name: "Power", exact: true }).click();
    await expect(page.getByRole("region", { name: "Live readings" })).toContainText("pdu-main");
    await expect(page.getByRole("region", { name: "Fans period statistics" })).toContainText("kW");
    await page.getByRole("button", { name: "Hashboards", exact: true }).click();
    await expect(page.getByText("Hashboard distribution unavailable.")).toBeVisible();
    await navigation.getByRole("link", { name: "Temperature", exact: true }).click();
    await expect(page.getByRole("region", { name: "Live readings" })).toContainText("°C");
    await page.getByRole("button", { name: "Fluid", exact: true }).click();
    await expect(page).toHaveURL(/temperature\?mode=fluid/);
    await expect(page.getByRole("list", { name: "Readings by tank" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Fluid" })).toHaveAttribute("aria-pressed", "true");
    expect(histories.length).toBeGreaterThan(0);
    expect(
      histories.every(
        (url) =>
          !url.searchParams.has("identifier") ||
          url.searchParams.get("metric") === "TIME_SERIES_METRIC_PLANT_FAN_POWER",
      ),
    ).toBe(true);
  });

  test("partial telemetry stays usable and small screens do not overflow", async ({ page }) => {
    await page.setViewportSize({ width: 393, height: 852 });
    await page.route("**/api/v2/telemetry/timeseries?**", async (route) => {
      const metric = new URL(route.request().url()).searchParams.get("metric");
      if (metric === "TIME_SERIES_METRIC_PLANT_FAN_POWER")
        await route.fulfill({ status: 404, contentType: "application/json", body: '{"message":"Metric unavailable"}' });
      else await route.continue();
    });
    await page.goto("/power-usage");
    await expect(page.getByRole("navigation", { name: "Container Home" })).toBeVisible();
    await expect(page.getByRole("region", { name: "Live readings" })).toContainText("pdu-main");
    await expect(page.getByRole("region", { name: "Fans period statistics" })).toContainText("—");
    const plot = page.getByRole("figure");
    await expect(plot).toBeVisible();
    expect((await plot.boundingBox())?.height).toBeGreaterThan(250);
    const fits = await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);
    expect(fits).toBe(true);
  });
});
