import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsSchedulesPage } from "../pages/settingsSchedules";

const SCHEDULE_PREFIX = "activity_schedule_e2e";

test.describe("Proto Fleet - Activity", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Delete schedules created during activity tests", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const viewport = testInfo.project.use?.viewport;
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

  test("Blink LEDs bulk action is visible in Activity with the right miner count", async ({
    activityPage,
    commonSteps,
    minersPage,
  }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      await minersPage.filterRigMiners();
    });

    await test.step("Select three miners and trigger Blink LEDs", async () => {
      await minersPage.clickMinerCheckboxByIndex(0);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckboxByIndex(1);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickMinerCheckboxByIndex(2);
      await minersPage.validateActionBarMinerCount(3);

      await minersPage.clickBlinkLEDsButton();
    });

    await test.step("Validate Blink LEDs toasts", async () => {
      await minersPage.validateTextInToastGroup("Blinking LEDs");
      await minersPage.validateTextInToastGroup("Blinked LEDs");
    });

    await test.step("Open Activity and filter by user", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.selectUserFilter(testConfig.users.admin.username);
    });

    await test.step("Validate the latest activity row", async () => {
      await activityPage.validateLatestActivityDescription("Blink LEDs");
      await activityPage.validateLatestActivityScope("3 miners");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
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
});
