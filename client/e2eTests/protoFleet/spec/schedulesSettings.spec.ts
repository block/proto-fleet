import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsSchedulesPage } from "../pages/settingsSchedules";

const SCHEDULE_PREFIX = "schedule_e2e";

test.describe("Proto Fleet - Schedules", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Delete schedules created during tests", async ({ browser }, testInfo) => {
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

  test("Create, pause/resume, edit, and delete a schedule", async ({ commonSteps, settingsSchedulesPage }) => {
    const scheduleName = generateRandomText(SCHEDULE_PREFIX);
    const updatedScheduleName = `${scheduleName}_updated`;

    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Navigate to schedules settings", async () => {
      await settingsSchedulesPage.navigateToSchedulesSettings();
      await settingsSchedulesPage.validateSchedulesPageOpened();
    });

    await test.step("Create a one-time schedule for one miner", async () => {
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
      await settingsSchedulesPage.validateScheduleStatus(scheduleName, "Active");
      await settingsSchedulesPage.validateScheduleAction(scheduleName, "Set power target");
      await settingsSchedulesPage.validateScheduleTargetSummary(scheduleName, "Applies to 1 miner");
    });

    await test.step("Pause and resume the schedule", async () => {
      await settingsSchedulesPage.pauseSchedule(scheduleName);
      await settingsSchedulesPage.validateScheduleStatus(scheduleName, "Paused");

      await settingsSchedulesPage.resumeSchedule(scheduleName);
      await settingsSchedulesPage.validateScheduleStatus(scheduleName, "Active");
    });

    await test.step("Edit the schedule name", async () => {
      await settingsSchedulesPage.openEditSchedule(scheduleName);
      await settingsSchedulesPage.inputScheduleName(updatedScheduleName);
      await settingsSchedulesPage.clickSaveSchedule();
      await settingsSchedulesPage.validateScheduleVisible(updatedScheduleName);
      await settingsSchedulesPage.validateScheduleNotVisible(scheduleName);
    });

    await test.step("Delete the schedule", async () => {
      await settingsSchedulesPage.deleteSchedule(updatedScheduleName);
    });
  });

  test("Recurring schedule validation", async ({ commonSteps, settingsSchedulesPage }) => {
    const scheduleName = generateRandomText(SCHEDULE_PREFIX);

    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Navigate to schedules settings", async () => {
      await settingsSchedulesPage.navigateToSchedulesSettings();
      await settingsSchedulesPage.validateSchedulesPageOpened();
    });

    await test.step("Switch to a recurring weekly schedule and validate days are required", async () => {
      await settingsSchedulesPage.clickAddSchedule();
      await settingsSchedulesPage.inputScheduleName(scheduleName);
      await settingsSchedulesPage.selectScheduleType("Recurring");
      await settingsSchedulesPage.selectScheduleFrequency("Weekly");
      await settingsSchedulesPage.validateSaveDisabled();
      await settingsSchedulesPage.selectWeekday("Monday");
      await settingsSchedulesPage.validateSaveEnabled();
    });

    await test.step("Validate monthly day-of-month input", async () => {
      await settingsSchedulesPage.selectScheduleFrequency("Monthly");
      await settingsSchedulesPage.inputDayOfMonth("0");
      await settingsSchedulesPage.validateValidationMessage("Enter a day between 1 and 31");
      await settingsSchedulesPage.validateSaveDisabled();
      await settingsSchedulesPage.inputDayOfMonth("15");
      await settingsSchedulesPage.validateSaveEnabled();
    });

    await test.step("Save the recurring schedule", async () => {
      await settingsSchedulesPage.clickSaveSchedule();
    });

    await test.step("Validate the recurring schedule summary", async () => {
      await settingsSchedulesPage.validateScheduleVisible(scheduleName);
      await settingsSchedulesPage.validateScheduleStatus(scheduleName, "Active");
      await settingsSchedulesPage.validateScheduleSummary(scheduleName, "15th day of month");
    });
  });
});
