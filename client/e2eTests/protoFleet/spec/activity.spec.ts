import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsSchedulesPage } from "../pages/settingsSchedules";

const SCHEDULE_PREFIX = "activity_schedule_e2e";
const LOGIN_EVENTS_FOR_PAGINATION = 55;

async function loginAsAdmin(authPage: AuthPage) {
  await authPage.inputUsername(testConfig.users.admin.username);
  await authPage.inputPassword(testConfig.users.admin.password);
  await authPage.clickLogin();
  await authPage.validateLoggedIn();
}

async function logoutAndLoginAsAdmin(authPage: AuthPage) {
  await authPage.logout();
  await authPage.validateRedirectedToAuth();
  await loginAsAdmin(authPage);
}

async function attemptFailedAdminLogin(authPage: AuthPage) {
  await authPage.inputUsername(testConfig.users.admin.username);
  await authPage.inputPassword(`${testConfig.users.admin.password}-wrong`);
  await authPage.clickLogin();
  await authPage.validateInvalidCredentials();
}

test.describe("Proto Fleet - Activity", () => {
  test.afterEach("CLEANUP: Delete schedules created during activity tests", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const viewport = testInfo.project.use?.viewport ?? undefined;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl, viewport });

    try {
      await test.step("Open a cleanup session for schedules", async () => {
        const page = await context.newPage();
        await page.goto("/");

        const authPage = new AuthPage(page, isMobile);
        const minersPage = new MinersPage(page, isMobile);
        const settingsSchedulesPage = new SettingsSchedulesPage(page, isMobile);
        const commonSteps = new CommonSteps(authPage, minersPage);

        await commonSteps.loginAsAdmin();
        await settingsSchedulesPage.navigateToSchedulesSettings();
        await settingsSchedulesPage.deleteSchedulesByPrefix(SCHEDULE_PREFIX);
      });
    } finally {
      await context.close();
    }
  });

  test("Blink LEDs activity opens detail modal and shows per-miner results", async ({
    page,
    activityPage,
    commonSteps,
    minersPage,
  }) => {
    let selectedMinerIps: string[] = [];

    await test.step("Log in, open Miners, and trigger Blink LEDs for three authenticated miners", async () => {
      await page.goto("/");
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();

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
      await activityPage.waitForCompletedActivityDetails(3, 0, 3);
      await activityPage.validateActivityDetailSucceededCount(3);
      await activityPage.validateActivityDetailFailedCount(0);
      for (const minerIp of selectedMinerIps) {
        await activityPage.validateActivityDetailMinerResultVisible(minerIp, "Success");
      }
      await activityPage.closeActivityDetails();
    });
  });

  test("Search, no-results, and clear-filters work for schedule activity", async ({
    page,
    activityPage,
    commonSteps,
    settingsSchedulesPage,
  }) => {
    const scheduleName = generateRandomText(SCHEDULE_PREFIX);

    await test.step("Log in and open schedules settings", async () => {
      await page.goto("/");
      await commonSteps.loginAsAdmin();
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
    page,
    activityPage,
    commonSteps,
    settingsSchedulesPage,
  }) => {
    const scheduleName = generateRandomText(SCHEDULE_PREFIX);

    await test.step("Log in and create a uniquely named schedule for Activity filtering", async () => {
      await page.goto("/");
      await commonSteps.loginAsAdmin();
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

  test("Activity export starts a CSV download", async ({ page, activityPage, commonSteps }) => {
    await test.step("Log in and open Activity with visible rows", async () => {
      await page.goto("/");
      await commonSteps.loginAsAdmin();
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.validateAnyActivityRowsVisible();
    });

    await test.step("Export activity as CSV", async () => {
      const download = await activityPage.exportCsvAndWaitForDownload();
      test.expect(download.suggestedFilename()).toMatch(/activity-export.*\.csv$/i);
    });
  });

  test("Activity load more appends older rows", async ({ page, activityPage, authPage, commonSteps }) => {
    await test.step("Log in and create enough recent login activity in one session", async () => {
      await page.goto("/");
      await commonSteps.loginAsAdmin();
      for (let i = 0; i < LOGIN_EVENTS_FOR_PAGINATION; i++) {
        await logoutAndLoginAsAdmin(authPage);
      }
    });

    await test.step("Open Activity and filter to admin logins", async () => {
      await activityPage.openActivityPageDirectly();
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

  test("Failed and successful admin logins appear in Activity as expected", async ({
    page,
    authPage,
    activityPage,
  }) => {
    await test.step("Open Fleet and log in as admin", async () => {
      await page.goto("/");
      await loginAsAdmin(authPage);
    });

    await test.step("Check the current successful login activity", async () => {
      await activityPage.openActivityPageDirectly();
      await activityPage.selectTypeFilter("Login");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateLatestActivityDescription("Login");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
    });

    await test.step("Log out, attempt a failed login, then sign in successfully again", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
      await attemptFailedAdminLogin(authPage);
      await page.goto("/auth");
      await loginAsAdmin(authPage);
    });

    await test.step("Filter Activity to the failed login row", async () => {
      await activityPage.openActivityPageDirectly();
      await activityPage.searchActivity("Login failed");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateFailedActivityRowVisible("Login failed", testConfig.users.admin.username);
    });

    await test.step("Open the failed activity details and validate the error", async () => {
      await activityPage.openFailedActivityDetails("Login failed", testConfig.users.admin.username);
      await activityPage.validateActivityDetailResult("Failure");
      await activityPage.validateActivityDetailError("invalid credentials");
      await activityPage.closeActivityDetails();
    });

    await test.step("Verify the successful login activity is still visible", async () => {
      await activityPage.openActivityPageDirectly();
      await activityPage.selectTypeFilter("Login");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateLatestActivityDescription("Login");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
    });
  });
});
