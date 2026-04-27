import { test } from "../fixtures/pageFixtures";

test.describe("Home dashboard", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
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

  test("Chart hashboard filtering", async ({ homePage, page }) => {
    await test.step("Initial state: all filters inactive", async () => {
      await homePage.validateFilteredChart([]);
    });

    await test.step("Click all hashboards (should enable all hashboards)", async () => {
      await page.getByTestId("chart-filter-all-hashboards").click();
      await homePage.validateFilteredChart(["1", "2", "3", "4"]);
    });

    await test.step("Click summary (should enable all)", async () => {
      await page.getByTestId("chart-filter-summary").click();
      await homePage.validateFilteredChart(["S", "1", "2", "3", "4"]);
    });

    await test.step("Click hashboard 3 (should leave S,1,2,4)", async () => {
      await page.getByTestId("chart-filter-hashboard-3").click();
      await homePage.validateFilteredChart(["S", "1", "2", "4"]);
    });

    await test.step("Click all hashboards (should re-enable all)", async () => {
      await page.getByTestId("chart-filter-all-hashboards").click();
      await homePage.validateFilteredChart(["S", "1", "2", "3", "4"]);
    });

    await test.step("Click all hashboards again (should leave only summary)", async () => {
      await page.getByTestId("chart-filter-all-hashboards").click();
      await homePage.validateFilteredChart(["S"]);
    });

    await test.step("Click summary (should re-enable all)", async () => {
      await page.getByTestId("chart-filter-summary").click();
      await homePage.validateFilteredChart([]);
    });

    await test.step("Click hashboard 4 (should leave only 4)", async () => {
      await page.getByTestId("chart-filter-hashboard-4").click();
      await homePage.validateFilteredChart(["4"]);
    });

    await test.step("Click hashboard 2 (should select 2 and 4)", async () => {
      await page.getByTestId("chart-filter-hashboard-2").click();
      await homePage.validateFilteredChart(["2", "4"]);
    });

    await test.step("Click summary (should add summary to 2 and 4)", async () => {
      await page.getByTestId("chart-filter-summary").click();
      await homePage.validateFilteredChart(["S", "2", "4"]);
    });

    await test.step("Click hashboard 3 (should add 3)", async () => {
      await page.getByTestId("chart-filter-hashboard-3").click();
      await homePage.validateFilteredChart(["S", "2", "3", "4"]);
    });

    await test.step("Click hashboard 1 (should add 1, all active)", async () => {
      await page.getByTestId("chart-filter-hashboard-1").click();
      await homePage.validateFilteredChart(["S", "1", "2", "3", "4"]);
    });
  });
});
