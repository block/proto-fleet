import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsSchedulesPage } from "../pages/settingsSchedules";

const SCHEDULE_PREFIX = "activity_schedule_e2e";

test.describe("Proto Fleet - Activity", () => {
  let shouldCleanupSchedules = false;

  test.beforeEach(async ({ page }) => {
    shouldCleanupSchedules = false;
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Delete schedules created during activity tests", async ({ browser }, testInfo) => {
    if (!shouldCleanupSchedules) {
      return;
    }

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

    await test.step("Filter to Proto rig miners", async () => {
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
      await activityPage.validateLatestActivityDescription("Blinked LEDs");
      await activityPage.validateLatestActivityScope("3 miners");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
    });
  });

  test("Blink LEDs activity detail modal shows batch summary", async ({ activityPage, commonSteps, minersPage }) => {
    await test.step("Trigger Blink LEDs for three Proto rig miners", async () => {
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      await minersPage.waitForMinersListToLoad();

      for (let index = 0; index < 3; index++) {
        await minersPage.clickMinerCheckboxByIndex(index);
        await minersPage.validateActionBarMinerCount(index + 1);
      }

      await minersPage.clickBlinkLEDsButton();
      await minersPage.validateTextInToastGroup("Blinking LEDs");
      await minersPage.validateTextInToastGroup("Blinked LEDs");
    });

    await test.step("Open Activity and narrow to the Blink LEDs batch", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.searchActivity("Blink LEDs");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
    });

    await test.step("Validate the Blink LEDs activity row", async () => {
      await activityPage.validateLatestActivityDescription("Blinked LEDs");
      await activityPage.validateLatestActivityScope("3 miners");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
    });

    await test.step("Open the detail modal and validate the batch results", async () => {
      await activityPage.openLatestActivityDetails();
      await activityPage.validateActivityDetailModalOpened();
      await activityPage.validateActivityDetailContainsText("Blinked LEDs");
      await activityPage.validateActivityDetailContainsText(testConfig.users.admin.username);
      await activityPage.validateActivityDetailContainsText("3 miners");
      await activityPage.validateActivityDetailContainsText("3/3 miners completed");

      await activityPage.dismissActivityDetailModal();
    });
  });

  test("Type and user filter pills can be removed and Activity export starts a CSV download", async ({
    page,
    activityPage,
    commonSteps,
  }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Open Activity and apply type and user filters", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.selectTypeFilter("Log in");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
    });

    await test.step("Validate and remove the type filter pill", async () => {
      await activityPage.validateFilterPillVisible("Log in");
      await activityPage.validateFilterPillVisible(testConfig.users.admin.username);
      await activityPage.removeFilterPill("Log in");
      await activityPage.validateFilterPillNotVisible("Log in");
      await activityPage.validateFilterPillVisible(testConfig.users.admin.username);
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
    });

    await test.step("Export the filtered activity list", async () => {
      const download = await activityPage.exportCsv();
      test.expect(download.suggestedFilename()).toMatch(/activity-export.*\.csv$/i);
    });

    await test.step("Keep the list stable after export", async () => {
      await page.bringToFront();
      await activityPage.waitForActivityListToLoad();
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
    });
  });

  test("Scope filter pills can be removed for group activity", async ({ activityPage, commonSteps, groupsPage }) => {
    const groupName = generateRandomText("activity_group");

    try {
      await test.step("Create a group that will appear in Activity", async () => {
        await commonSteps.loginAsAdmin();
        await groupsPage.navigateToGroupsPage();
        await groupsPage.clickAddGroupButton();
        await groupsPage.inputGroupName(groupName);
        await groupsPage.waitForModalListToLoad();
        await groupsPage.selectMinersByIndex([0]);
        await groupsPage.clickSaveInModal();
        await groupsPage.validateTextInToast(`Group "${groupName}" created`);
      });

      await test.step("Open Activity and apply the group scope filter", async () => {
        await activityPage.navigateToActivityPage();
        await activityPage.waitForActivityListToLoad();
        await activityPage.selectScopeFilter("Group");
        await activityPage.searchActivity(groupName);
      });

      await test.step("Validate and remove the scope filter pill", async () => {
        await activityPage.validateFilterPillVisible("Group");
        await activityPage.validateActivityDescriptionVisible(`Created group: ${groupName}`);
        await activityPage.removeFilterPill("Group");
        await activityPage.validateFilterPillNotVisible("Group");
        await activityPage.validateActivityDescriptionVisible(`Created group: ${groupName}`);
      });
    } finally {
      await groupsPage.navigateToGroupsPage();
      await groupsPage.deleteSavedGroupIfVisible(groupName);
    }
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
      shouldCleanupSchedules = true;
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
