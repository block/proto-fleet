/* eslint-disable playwright/expect-expect */
import { test } from "../fixtures/pageFixtures";

const FLEET_DURATIONS = ["1h", "24h", "7d", "30d", "90d", "1y"] as const;
const DURATION_SWITCH_TARGETS = ["7d", "30d"] as const;

test.describe("Proto Fleet - Dashboard", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Dashboard renders the paired fleet shell", async ({ homePage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Validate dashboard sections are visible", async () => {
      await homePage.validateHomePageOpened();
      await homePage.validateDashboardSectionVisible("Overview");
      await homePage.validateDashboardSectionVisible("Performance");
    });

    await test.step("Validate dashboard panels are visible", async () => {
      await homePage.validateDashboardPanelVisible("Hashrate");
      await homePage.validateDashboardPanelVisible("Uptime");
      await homePage.validateDashboardPanelVisible("Temperature");
      await homePage.validateDashboardPanelVisible("Power");
      await homePage.validateDashboardPanelVisible("Efficiency");
    });

    await test.step("Validate setup prompt is not shown for the prepared fleet", async () => {
      await homePage.validateCompleteSetupTitleNotVisible();
      await homePage.validateSetupTaskCardNotVisible("Authenticate miners");
      await homePage.validateSetupTaskCardNotVisible("Configure pools");
      await homePage.validateAuthenticateMinersButtonNotVisible();
      await homePage.validateConfigurePoolsButtonNotVisible();
      await homePage.validateDashboardPerformanceDisclaimerVisible();
    });
  });

  test("Dashboard duration selection persists after refresh", async ({ homePage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    let currentDuration: string | null = null;
    let targetDuration = "7d";

    await test.step("Choose a different dashboard duration", async () => {
      currentDuration = await homePage.getSelectedDuration(FLEET_DURATIONS);
      targetDuration =
        DURATION_SWITCH_TARGETS.find((duration) => duration !== currentDuration) ?? DURATION_SWITCH_TARGETS[0];

      await homePage.clickDurationButton(targetDuration);
      await homePage.validateDurationSelected(targetDuration);
    });

    await test.step("Validate dashboard still renders after changing duration", async () => {
      await homePage.validateDashboardPanelVisible("Hashrate");
      await homePage.validateDashboardPanelVisible("Uptime");
      await homePage.validateDashboardPanelVisible("Temperature");
      await homePage.validateDashboardPanelVisible("Power");
      await homePage.validateDashboardPanelVisible("Efficiency");
    });

    await test.step("Refresh and validate duration persistence", async () => {
      await homePage.reloadPage();
      await homePage.validateHomePageOpened();
      await homePage.validateDurationSelected(targetDuration);
      await homePage.validateDashboardPanelVisible("Hashrate");
      await homePage.validateDashboardPanelVisible("Uptime");
      await homePage.validateDashboardPanelVisible("Temperature");
      await homePage.validateDashboardPanelVisible("Power");
      await homePage.validateDashboardPanelVisible("Efficiency");
    });
  });

  test("Dashboard duration selection persists after issue-card navigation", async ({
    homePage,
    minersPage,
    commonSteps,
  }) => {
    await commonSteps.loginAsAdmin();

    let currentDuration: string | null = null;
    let targetDuration = "7d";

    await test.step("Choose a different dashboard duration", async () => {
      currentDuration = await homePage.getSelectedDuration(FLEET_DURATIONS);
      targetDuration =
        DURATION_SWITCH_TARGETS.find((duration) => duration !== currentDuration) ?? DURATION_SWITCH_TARGETS[0];

      await homePage.clickDurationButton(targetDuration);
      await homePage.validateDurationSelected(targetDuration);
    });

    await test.step("Navigate to miners from the Control Boards issue card", async () => {
      await homePage.clickControlBoardsLink();
      await minersPage.validateMinersPageOpened();
      await minersPage.validateActiveFilter("Control board issue");
    });

    await test.step("Return home and validate dashboard state", async () => {
      await minersPage.navigateToHomePage();
      await homePage.validateHomePageOpened();
      await homePage.validateDurationSelected(targetDuration);
      await homePage.validateDashboardSectionVisible("Overview");
      await homePage.validateDashboardSectionVisible("Performance");
      await homePage.validateDashboardPanelVisible("Hashrate");
      await homePage.validateDashboardPanelVisible("Uptime");
      await homePage.validateDashboardPanelVisible("Temperature");
      await homePage.validateDashboardPanelVisible("Power");
      await homePage.validateDashboardPanelVisible("Efficiency");
    });
  });
});
