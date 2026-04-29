import { expect, test } from "../fixtures/pageFixtures";

test.describe("Logs", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
  });

  test("Search and log-type filters work", async ({ commonSteps, logsPage }) => {
    let initialRowCount = 0;
    let initialErrorCount = 0;
    let initialWarningCount = 0;
    let searchableQuery = "";
    const noMatchQuery = `no-match-${Date.now()}`;

    await test.step("Open Logs and validate the page shell", async () => {
      await commonSteps.navigateToLogs();
      await logsPage.validateLogsPageOpened();
      await logsPage.waitForLogsListToBeReady();
      await logsPage.validateLogRowsVisible();
      initialRowCount = await logsPage.getLogRowCount();
      initialErrorCount = await logsPage.getLogRowCountByType("error");
      initialWarningCount = await logsPage.getLogRowCountByType("warn");
      searchableQuery = await logsPage.getSearchableSubstringFromFirstRow();

      expect(initialRowCount).toBeGreaterThan(0);
    });

    await test.step("Filter by errors", async () => {
      await logsPage.clickErrorFilter();

      // eslint-disable-next-line playwright/no-conditional-in-test
      if (initialErrorCount > 0) {
        await logsPage.validateOnlyLogTypeVisible("error");
      } else {
        await logsPage.validateNoResultsState("No errors found");
      }

      await logsPage.clickErrorFilter();
      await logsPage.validateLogRowsVisible();
    });

    await test.step("Filter by warnings", async () => {
      await logsPage.clickWarningFilter();

      // eslint-disable-next-line playwright/no-conditional-in-test
      if (initialWarningCount > 0) {
        await logsPage.validateOnlyLogTypeVisible("warn");
      } else {
        await logsPage.validateNoResultsState("No warnings found");
      }

      await logsPage.clickWarningFilter();
      await logsPage.validateLogRowsVisible();
    });

    await test.step("Search matching logs", async () => {
      await logsPage.searchLogs(searchableQuery);
      await logsPage.validateLogRowsVisible();
      expect(await logsPage.getLogRowCount()).toBeGreaterThan(0);
    });

    await test.step("Search with no matches and validate filtered empty state", async () => {
      await logsPage.searchLogs(noMatchQuery);
      await logsPage.validateNoResultsState(`No results match “${noMatchQuery}”`);
    });

    await test.step("Combine search and error filter empty state", async () => {
      await logsPage.clickErrorFilter();
      await logsPage.validateNoResultsState(`No errors match “${noMatchQuery}”`);
      await logsPage.clickErrorFilter();
    });

    await test.step("Clear search and restore logs", async () => {
      await logsPage.clearSearch();
      await logsPage.validateLogRowsVisible();
      expect(await logsPage.getLogRowCount()).toBeGreaterThanOrEqual(initialRowCount);
    });
  });

  test("Export logs starts a download", async ({ commonSteps, logsPage, page }) => {
    await commonSteps.navigateToLogs();
    await logsPage.validateLogsPageOpened();
    await logsPage.waitForLogsListToBeReady();

    const downloadPromise = page.waitForEvent("download");

    await test.step("Start a logs export", async () => {
      await page.getByRole("button", { name: "Export" }).click();
    });

    await test.step("Validate the download starts", async () => {
      const download = await downloadPromise;
      expect(download.suggestedFilename()).toMatch(/miner-logs.*\.csv$/i);
    });
  });
});
