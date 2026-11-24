import { fromBinary } from "@bufbuild/protobuf";
import { expect, test } from "@playwright/test";
import { TimeSeriesMetric } from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/common_pb";
import { QueryTimeSeriesRequestSchema } from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/telemetry_pb";

test.describe("Container Home", () => {
  test("System, metric modes and durations use aggregate requests", async ({ page }) => {
    const histories: { metric: TimeSeriesMetric; identifier: string }[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.pathname === "/mdk.v2.TelemetryService/QueryTimeSeries")
        histories.push(fromBinary(QueryTimeSeriesRequestSchema, request.postDataBuffer()!));
    });
    await page.goto("/?view=system");
    const navigation = page.getByRole("navigation", { name: "Container Home" });
    await expect(navigation.getByRole("link", { name: "System" })).toHaveAttribute("aria-current", "page");
    await expect(page.getByRole("heading", { name: "Container system" })).toBeAttached();
    await expect(page.getByRole("region", { name: "Container cooling system schematic" })).toBeVisible();
    await expect(page.getByRole("region", { name: "Live readings" })).toContainText("Container power");
    await expect(page.getByRole("figure")).toHaveCount(0);
    expect(histories).toHaveLength(0);
    await navigation.getByRole("link", { name: "Hashrate", exact: true }).click();
    await expect(page.locator(".recharts-line-curve")).toHaveCount(1);
    await page.getByRole("button", { name: "48h" }).click();
    await expect(page.getByRole("button", { name: "48h" })).toHaveAttribute("aria-pressed", "true");
    await navigation.getByRole("link", { name: "Power", exact: true }).click();
    await expect(page.getByRole("region", { name: "Live readings" })).not.toContainText("pdu-1");
    await expect(page.getByRole("region", { name: "Fans period statistics" })).toHaveCount(0);
    await expect(page.getByRole("group", { name: "Power chart mode" })).toHaveCount(0);
    const legend = page.getByRole("list", { name: "Latest series values" });
    await expect(legend).toContainText("Facility total");
    await expect(legend).toContainText("Mining power");
    await expect(legend).toContainText("pdu-1");
    const fans = legend.getByRole("listitem").filter({ has: page.getByText("Fans", { exact: true }) });
    await expect(fans).toContainText("kW");
    await expect(page.locator(".recharts-area")).toHaveCount(0);
    for (const label of ["Pumps", "AC", "Aux"]) await expect(page.getByText(label, { exact: true })).toHaveCount(0);
    await navigation.getByRole("link", { name: "Temperature", exact: true }).click();
    await expect(page.getByRole("region", { name: "Live readings" })).toContainText("°C");
    await page.getByRole("button", { name: "Fluid", exact: true }).click();
    await expect(page).toHaveURL(/temperature\?mode=fluid/);
    await expect(page.getByRole("list", { name: "Readings by tank" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Fluid" })).toHaveAttribute("aria-pressed", "true");
    expect(histories.length).toBeGreaterThan(0);
    expect(
      histories.every(
        (query) =>
          !query.identifier ||
          [TimeSeriesMetric.PLANT_FAN_POWER, TimeSeriesMetric.PLANT_DISTRIBUTION_POWER].includes(query.metric),
      ),
    ).toBe(true);
  });

  test("partial telemetry stays usable and small screens do not overflow", async ({ page }) => {
    await page.setViewportSize({ width: 393, height: 852 });
    await page.route("**/mdk.v2.TelemetryService/QueryTimeSeries", async (route) => {
      const { metric } = fromBinary(QueryTimeSeriesRequestSchema, route.request().postDataBuffer()!);
      if (metric === TimeSeriesMetric.PLANT_FAN_POWER)
        await route.fulfill({ status: 404, contentType: "application/json", body: '{"message":"Metric unavailable"}' });
      else await route.continue();
    });
    await page.goto("/power-usage");
    await expect(page.getByRole("navigation", { name: "Container Home" })).toBeVisible();
    await expect(page.getByRole("region", { name: "Live readings" })).not.toContainText("pdu-1");
    const fans = page
      .getByRole("list", { name: "Latest series values" })
      .getByRole("listitem")
      .filter({ has: page.getByText("Fans", { exact: true }) });
    await expect(fans).toContainText("—");
    const plot = page.getByRole("figure");
    await expect(plot).toBeVisible();
    expect((await plot.boundingBox())?.height).toBeGreaterThan(250);
    const fits = await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);
    expect(fits).toBe(true);
  });
});
