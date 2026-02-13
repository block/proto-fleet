/* eslint-disable playwright/expect-expect */
import { test } from "../fixtures/pageFixtures";

test.describe("Home dashboard", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Validate KPI tabs and stats", async ({ homePage }) => {
    await test.step("Validate Hashrate stats", async () => {
      const expectedValuePattern = /(\d+,)?\d+\.\d\sTH\/(S|s)/;
      await homePage.validateTabHeading("hashrate", "Hashrate");
      await homePage.validateTabValue("hashrate", expectedValuePattern);
      await homePage.clickTab("hashrate");
      await homePage.validateStatsCount(4);
      await homePage.validateStatItem(0, "Average", expectedValuePattern);
      await homePage.validateStatItem(1, "Highest", expectedValuePattern);
      await homePage.validateStatItem(2, "Lowest", expectedValuePattern);
      await homePage.validateStatItem(3, "Lowest Performer", /Hashboard \d/);
      await homePage.hoverOverChart();
      await homePage.validateChartTooltipWithHashboards(expectedValuePattern);
    });

    await test.step("Validate Efficiency stats", async () => {
      const expectedValuePattern = /\d+\.\d\sJ\/TH/;
      await homePage.validateTabHeading("efficiency", "Efficiency");
      await homePage.validateTabValue("efficiency", expectedValuePattern);
      await homePage.clickTab("efficiency");
      await homePage.validateStatsCount(4);
      await homePage.validateStatItem(0, "Average", expectedValuePattern);
      await homePage.validateStatItem(1, "Highest", expectedValuePattern);
      await homePage.validateStatItem(2, "Lowest", expectedValuePattern);
      await homePage.validateStatItem(3, "Lowest Performer", /Hashboard \d/);
      await homePage.hoverOverChart();
      await homePage.validateChartTooltipWithHashboards(expectedValuePattern);
    });

    await test.step("Validate Power Usage stats", async () => {
      const expectedValuePattern = /\d+\.\d\skW/;
      const acceptableWattsPattern = /(\d|,)+\.\d\sk?W/;
      await homePage.validateTabHeading("powerUsage", "Power Usage");
      await homePage.validateTabValue("powerUsage", expectedValuePattern);
      await homePage.clickTab("powerUsage");
      await homePage.validateStatsCount(3);
      await homePage.validateStatItem(0, "Average", expectedValuePattern);
      await homePage.validateStatItem(1, "Highest", expectedValuePattern);
      await homePage.validateStatItem(2, "Lowest", expectedValuePattern);
      await homePage.hoverOverChart();
      await homePage.validateChartTooltipWithHashboards(acceptableWattsPattern);
    });

    await test.step("Validate Temperature stats", async () => {
      const expectedValuePattern = /\d+\.\d\s°(C|F)/;
      await homePage.validateTabHeading("temperature", "Temperature");
      await homePage.validateTabValue("temperature", expectedValuePattern);
      await homePage.clickTab("temperature");
      await homePage.validateStatsCount(4);
      await homePage.validateStatItem(0, "Average", expectedValuePattern);
      await homePage.validateStatItem(1, "Highest", expectedValuePattern);
      await homePage.validateStatItem(2, "Lowest", expectedValuePattern);
      await homePage.validateStatItem(3, "Hottest Hashboard", /Hashboard \d/);
      await homePage.hoverOverChart();
      await homePage.validateChartTooltipWithHashboards(expectedValuePattern);
    });
  });
});
