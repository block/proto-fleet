import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { recordFailedAdminLogin, seedAdminLoginActivities } from "../helpers/activityHelper";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsSchedulesPage } from "../pages/settingsSchedules";

const SCHEDULE_PREFIX = "activity_schedule_e2e";
const LOGIN_EVENTS_FOR_PAGINATION = 55;

test.describe("Proto Fleet - Activity", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Delete schedules created during activity tests", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const viewport = testInfo.project.use?.viewport ?? undefined;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl, viewport });

    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const settingsSchedulesPage = new SettingsSchedulesPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await settingsSchedulesPage.navigateToSchedulesSettings();
      await settingsSchedulesPage.deleteSchedulesByPrefix(SCHEDULE_PREFIX);
    } finally {
      await context.close();
    }
  });

  test("Blink LEDs activity opens detail modal and shows per-miner results", async ({
    activityPage,
    commonSteps,
    minersPage,
  }) => {
    let selectedMinerIps: string[] = [];

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Select three authenticated miners and trigger Blink LEDs", async () => {
      selectedMinerIps = [
        await minersPage.getAuthenticatedMinerIpAddressByIndex(0),
        await minersPage.getAuthenticatedMinerIpAddressByIndex(1),
        await minersPage.getAuthenticatedMinerIpAddressByIndex(2),
      ];

      await minersPage.clickMinerCheckbox(selectedMinerIps[0]);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(selectedMinerIps[1]);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickMinerCheckbox(selectedMinerIps[2]);
      await minersPage.validateActionBarMinerCount(3);

      await minersPage.clickBlinkLEDsButton();
    });

    await test.step("Open Activity and filter by user", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.selectUserFilter(testConfig.users.admin.username);
    });

    await test.step("Validate the Blink LEDs activity row", async () => {
      await activityPage.validateCompletedActivityRowVisible("Blink LEDs", "3 miners");
      await activityPage.validateCompletedActivityRowUser("Blink LEDs", "3 miners", testConfig.users.admin.username);
      await activityPage.validateCompletedActivityRowNotMarkedFailed("Blink LEDs", "3 miners");
    });

    await test.step("Open the detail modal and validate per-miner results", async () => {
      await activityPage.openCompletedActivityDetails("Blink LEDs", "3 miners");
      await activityPage.validateActivityDetailSucceededCount(3);
      await activityPage.validateActivityDetailFailedCount(0);
      for (const minerIp of selectedMinerIps) {
        await activityPage.validateActivityDetailMinerResultVisible(minerIp, "Success");
      }
      await activityPage.closeActivityDetails();
    });
  });

  test("Search, no-results, and clear-filters work for schedule activity", async ({
    activityPage,
    commonSteps,
    settingsSchedulesPage,
  }) => {
    const scheduleName = generateRandomText(SCHEDULE_PREFIX);

    await commonSteps.loginAsAdmin();

    await test.step("Open schedules settings", async () => {
      await settingsSchedulesPage.navigateToSchedulesSettings();
      await settingsSchedulesPage.validateSchedulesPageOpened();
    });

    await test.step("Create a uniquely named schedule", async () => {
      await settingsSchedulesPage.clickAddSchedule();
      await settingsSchedulesPage.inputScheduleName(scheduleName);
      await settingsSchedulesPage.selectStartDate(1);
      await settingsSchedulesPage.openMinersTargetSelector();
      await settingsSchedulesPage.waitForMinerSelectionModalToLoad();
      await settingsSchedulesPage.selectFirstMiners(1);
      await settingsSchedulesPage.confirmMinerSelection();
      await settingsSchedulesPage.clickSaveSchedule();
    });

    await test.step("Validate the schedule was created", async () => {
      await settingsSchedulesPage.validateScheduleVisible(scheduleName);
    });

    await test.step("Open Activity and search for the created schedule", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.searchActivity(scheduleName);
    });

    await test.step("Validate the searched schedule activity row", async () => {
      await activityPage.validateActivityDescriptionVisible(`Created schedule: ${scheduleName}`);
    });

    await test.step("Filter Activity by type and validate the same row", async () => {
      await activityPage.selectTypeFilter("Create schedule");
      await activityPage.validateActivityDescriptionVisible(`Created schedule: ${scheduleName}`);
    });

    await test.step("Search for a missing activity entry", async () => {
      await activityPage.searchActivity("missing-activity-entry");
      await activityPage.validateNoResultsVisible();
    });

    await test.step("Clear filters and validate results return", async () => {
      await activityPage.clearAllFilters();
      await activityPage.waitForActivityListToLoad();
      await activityPage.validateSearchInputValue("");
      await activityPage.validateActivityDescriptionVisible(`Created schedule: ${scheduleName}`);
    });
  });

  test("Activity filter pills can be removed individually", async ({
    activityPage,
    commonSteps,
    settingsSchedulesPage,
  }) => {
    const scheduleName = generateRandomText(SCHEDULE_PREFIX);

    await commonSteps.loginAsAdmin();

    await test.step("Create a uniquely named schedule for Activity filtering", async () => {
      await settingsSchedulesPage.navigateToSchedulesSettings();
      await settingsSchedulesPage.validateSchedulesPageOpened();
      await settingsSchedulesPage.clickAddSchedule();
      await settingsSchedulesPage.inputScheduleName(scheduleName);
      await settingsSchedulesPage.selectStartDate(1);
      await settingsSchedulesPage.openMinersTargetSelector();
      await settingsSchedulesPage.waitForMinerSelectionModalToLoad();
      await settingsSchedulesPage.selectFirstMiners(1);
      await settingsSchedulesPage.confirmMinerSelection();
      await settingsSchedulesPage.clickSaveSchedule();
      await settingsSchedulesPage.validateScheduleVisible(scheduleName);
    });

    await test.step("Apply Activity filters that generate removable pills", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.searchActivity(scheduleName);
      await activityPage.selectTypeFilter("Create schedule");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateActivityDescriptionVisible(`Created schedule: ${scheduleName}`);
      await activityPage.validateFilterPillVisible("Create schedule");
      await activityPage.validateFilterPillVisible(testConfig.users.admin.username);
    });

    await test.step("Remove the type pill and keep the user pill active", async () => {
      await activityPage.removeFilterPill("Create schedule");
      await activityPage.validateFilterPillNotVisible("Create schedule");
      await activityPage.validateFilterPillVisible(testConfig.users.admin.username);
      await activityPage.validateActivityDescriptionVisible(`Created schedule: ${scheduleName}`);
    });

    await test.step("Remove the user pill and leave the search state intact", async () => {
      await activityPage.removeFilterPill(testConfig.users.admin.username);
      await activityPage.validateFilterPillNotVisible(testConfig.users.admin.username);
      await activityPage.validateSearchInputValue(scheduleName);
      await activityPage.validateActivityDescriptionVisible(`Created schedule: ${scheduleName}`);
    });
  });

  test("Activity export starts a CSV download", async ({ activityPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Open Activity with visible rows", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.validateAnyActivityRowsVisible();
    });

    await test.step("Export activity as CSV", async () => {
      const download = await activityPage.exportCsvAndWaitForDownload();
      test.expect(download.suggestedFilename()).toMatch(/activity-export.*\.csv$/i);
    });
  });

  test("Activity load more appends older rows", async ({ activityPage, browser, commonSteps }, testInfo) => {
    const viewport = testInfo.project.use?.viewport ?? undefined;
    const isMobile = testInfo.project.use?.isMobile ?? false;

    await commonSteps.loginAsAdmin();
    await seedAdminLoginActivities(browser, {
      count: LOGIN_EVENTS_FOR_PAGINATION,
      isMobile,
      viewport,
    });

    await test.step("Open Activity and filter to admin logins", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.selectTypeFilter("Login");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateLoadMoreVisible();
    });

    await test.step("Load more activity rows and validate the list grows", async () => {
      const initialRowCount = await activityPage.getVisibleActivityRowCount();
      test.expect(initialRowCount).toBeGreaterThan(0);

      await activityPage.clickLoadMore(initialRowCount);

      const expandedRowCount = await activityPage.getVisibleActivityRowCount();
      test.expect(expandedRowCount).toBeGreaterThan(initialRowCount);
    });
  });
});

test.describe("Proto Fleet - Activity Login", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Login activity is visible for the signed-in admin", async ({ authPage, activityPage }) => {
    await test.step("Log in as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Open Activity and filter to login events", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.selectTypeFilter("Login");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
    });

    await test.step("Validate the latest login row", async () => {
      await activityPage.validateLatestActivityDescription("Login");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
    });
  });

  test("Failed activity row is marked failed and opens correct details", async ({
    activityPage,
    authPage,
    browser,
  }, testInfo) => {
    const viewport = testInfo.project.use?.viewport ?? undefined;
    const isMobile = testInfo.project.use?.isMobile ?? false;

    await test.step("Record a failed admin login, then sign in successfully", async () => {
      await recordFailedAdminLogin(browser, { isMobile, viewport });
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Filter Activity to the failed login row", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.searchActivity("Login failed");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateLatestActivityDescription("Login failed");
      await activityPage.validateLatestActivityMarkedFailed();
    });

    await test.step("Open the failed activity details and validate the error", async () => {
      await activityPage.openLatestActivityDetails();
      await activityPage.validateActivityDetailResult("Failure");
      await activityPage.validateActivityDetailError("invalid credentials");
      await activityPage.closeActivityDetails();
    });
  });
});
