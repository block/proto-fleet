import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { GroupsPage } from "../pages/groups";
import { MinersPage } from "../pages/miners";
import { SettingsSchedulesPage } from "../pages/settingsSchedules";

const SCHEDULE_PREFIX = "activity_schedule_e2e";

async function triggerBlinkLedsActivity(commonSteps: CommonSteps, minersPage: MinersPage) {
  await commonSteps.loginAsAdmin();
  await commonSteps.goToMinersPage();
  await minersPage.filterRigMiners();
  await minersPage.waitForMinersListToLoad();

  const selectedMinerIps: string[] = [];

  for (let index = 0; index < 3; index++) {
    selectedMinerIps.push(await minersPage.getMinerIpAddressByIndex(index));
    await minersPage.clickMinerCheckboxByIndex(index);
    await minersPage.validateActionBarMinerCount(index + 1);
  }

  await minersPage.clickBlinkLEDsButton();
  await minersPage.validateTextInToastGroup("Blinking LEDs");
  await minersPage.validateTextInToastGroup("Blinked LEDs");

  return selectedMinerIps;
}

async function createGroupActivity(commonSteps: CommonSteps, groupsPage: GroupsPage, groupName: string) {
  await commonSteps.loginAsAdmin();
  await groupsPage.navigateToGroupsPage();
  await groupsPage.clickAddGroupButton();
  await groupsPage.inputGroupName(groupName);
  await groupsPage.waitForModalListToLoad();
  await groupsPage.selectMinersByIndex([0]);
  await groupsPage.clickSaveInModal();
  await groupsPage.validateTextInToast(`Group "${groupName}" created`);
}

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

  test("Blink LEDs activity detail modal shows batch summary and per-miner results", async ({
    activityPage,
    commonSteps,
    minersPage,
  }) => {
    const selectedMinerIps = await triggerBlinkLedsActivity(commonSteps, minersPage);

    await test.step("Open Activity and narrow to the Blink LEDs batch", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.searchActivity("Blink LEDs");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
    });

    await test.step("Validate the Blink LEDs activity row", async () => {
      await activityPage.validateLatestActivityDescription("Blink LEDs");
      await activityPage.validateLatestActivityScope("3 miners");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
    });

    await test.step("Open the detail modal and validate the batch results", async () => {
      await activityPage.openLatestActivityDetails();
      await activityPage.validateActivityDetailModalOpened();
      await activityPage.validateActivityDetailContainsText("Blink led");
      await activityPage.validateActivityDetailContainsText(testConfig.users.admin.username);
      await activityPage.validateActivityDetailContainsText("Success");
      await activityPage.validateActivityDetailContainsText("Succeeded");
      await activityPage.validateActivityDetailContainsText("Failed");
      await activityPage.validateActivityDetailContainsText("3 miners");
      await activityPage.validateActivityDetailContainsText("0 miners");
      await activityPage.validateActivityDetailDeviceResultsRowCount(3);

      for (const minerIp of selectedMinerIps) {
        await activityPage.validateActivityDetailContainsText(minerIp);
      }

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
      await activityPage.selectTypeFilter("Login");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
    });

    await test.step("Validate and remove the type filter pill", async () => {
      await activityPage.validateFilterPillVisible("Login");
      await activityPage.validateFilterPillVisible(testConfig.users.admin.username);
      await activityPage.removeFilterPill("Login");
      await activityPage.validateFilterPillNotVisible("Login");
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
      await createGroupActivity(commonSteps, groupsPage, groupName);

      await test.step("Open Activity and apply the group scope filter", async () => {
        await activityPage.navigateToActivityPage();
        await activityPage.waitForActivityListToLoad();
        await activityPage.selectScopeFilter("Group");
        await activityPage.searchActivity(groupName);
      });

      await test.step("Validate and remove the scope filter pill", async () => {
        await activityPage.validateFilterPillVisible("Group");
        await activityPage.validateActivityDescriptionVisible(`Create group: ${groupName}`);
        await activityPage.removeFilterPill("Group");
        await activityPage.validateFilterPillNotVisible("Group");
        await activityPage.validateActivityDescriptionVisible(`Create group: ${groupName}`);
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

  test("Failed login activity is visible after correcting invalid credentials and signing in", async ({
    authPage,
    activityPage,
  }) => {
    await test.step("Log in as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Open Activity and validate the latest login row", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.selectTypeFilter("Login");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateLatestActivityDescription("Login");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
    });

    await test.step("Log out", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Attempt login with an invalid password and validate the error", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(`${testConfig.users.admin.password}-invalid`);
      await authPage.clickLogin();
      await authPage.validateInvalidCredentials();
    });

    await test.step("Rewrite the correct password and validate the error clears", async () => {
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.validateInvalidCredentialsNotVisible();
    });

    await test.step("Log in successfully with corrected credentials", async () => {
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Validate the failed login attempt appears in Activity", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.searchActivity("Login failed");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateLatestActivityDescription("Login failed");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityMarkedFailed();
    });
  });
});
